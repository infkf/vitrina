package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vitrina/internal/config"
	"vitrina/internal/deploy"
	"vitrina/internal/output"
	"vitrina/internal/registry"

	"github.com/spf13/cobra"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Run a background monitoring daemon for all apps",
	Long: `Runs as a long-lived process that periodically checks app health,
TLS certificate expiry, and container status.

Writes a status file to /etc/vitrina/monitor.json on each check cycle.
Use --once to run a single check and exit.`,
	RunE: runRemoteOrLocal(runMonitor),
}

var (
	monitorInterval time.Duration
	monitorOnce     bool
)

func init() {
	monitorCmd.Flags().DurationVarP(&monitorInterval, "interval", "i", 5*time.Minute,
		"Check interval (e.g. 5m, 1h)")
	monitorCmd.Flags().BoolVar(&monitorOnce, "once", false,
		"Run a single check and exit")
	rootCmd.AddCommand(monitorCmd)
}

type monitorResult struct {
	CheckedAt string             `json:"checked_at"`
	Apps      []monitorAppResult `json:"apps"`
}

type monitorAppResult struct {
	Subdomain    string `json:"subdomain"`
	FQDN         string `json:"fqdn"`
	Port         int    `json:"port"`
	Status       string `json:"status"`
	HTTPCode     int    `json:"http_code,omitempty"`
	TLSExpiry    string `json:"tls_expiry,omitempty"`
	TLSDaysLeft  int    `json:"tls_days_left,omitempty"`
	ContainersUp bool   `json:"containers_up"`
	Error        string `json:"error,omitempty"`
	baseStatus   string
}

func runMonitor(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	runChecks()

	if monitorOnce {
		return nil
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	fmt.Fprintf(os.Stderr, "Vitrina monitor running (interval: %v)\n", monitorInterval)

	for {
		select {
		case <-ticker.C:
			runChecks()
		case sig := <-sigCh:
			fmt.Fprintf(os.Stderr, "\nVitrina monitor stopped (%v)\n", sig)
			return nil
		}
	}
}

func runChecks() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[monitor] config load error: %v\n", err)
		return
	}

	store := registry.New()
	apps, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[monitor] registry error: %v\n", err)
		return
	}

	var results []monitorAppResult
	now := time.Now()
	hasWarnings := false

	for _, app := range apps {
		r := monitorAppResult{
			Subdomain: app.Subdomain,
			FQDN:      app.FQDN,
			Port:      app.Port,
		}

		r.ContainersUp = checkContainers(cfg, app)

		tlsExpiry, tlsErr := checkTLS(app.FQDN, 10*time.Second)
		if tlsErr != nil {
			r.Status = "unreachable"
			r.Error = tlsErr.Error()
		} else {
			r.TLSExpiry = tlsExpiry.Format(time.RFC3339)
			r.TLSDaysLeft = int(time.Until(tlsExpiry).Hours() / 24)

			code, httpErr := checkHTTP(app.FQDN, app.HealthPath, 10*time.Second)
			if httpErr != nil {
				r.Status = "tls-ok/http-down"
				r.Error = httpErr.Error()
			} else {
				r.HTTPCode = code
				if code >= 200 && code < 400 {
					r.Status = "healthy"
				} else {
					r.Status = "unhealthy"
				}
			}
		}

		r.baseStatus = r.Status

		if r.TLSDaysLeft >= 0 && r.TLSDaysLeft <= 14 {
			r.Status += fmt.Sprintf(" (cert expires in %dd)", r.TLSDaysLeft)
			hasWarnings = true
		}

		results = append(results, r)
	}

	writeMonitorStatus(now, results)
	printMonitorSummary(results, hasWarnings)
}

func checkContainers(cfg *config.Config, app *registry.App) bool {
	appDir := cfg.AppsDir + "/" + app.Subdomain
	if !deploy.HasDockerCompose(appDir) && !deploy.HasDockerfile(appDir) {
		return false
	}
	return deploy.ComposeIsRunning(appDir)
}

func checkTLS(fqdn string, timeout time.Duration) (time.Time, error) {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: timeout},
		"tcp",
		fqdn+":443",
		&tls.Config{InsecureSkipVerify: false},
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("TLS dial: %v", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return time.Time{}, fmt.Errorf("no certificates presented")
	}

	var earliest time.Time
	for _, cert := range certs {
		if earliest.IsZero() || cert.NotAfter.Before(earliest) {
			earliest = cert.NotAfter
		}
	}
	return earliest, nil
}

func checkHTTP(fqdn, healthPath string, timeout time.Duration) (int, error) {
	if healthPath == "" {
		healthPath = "/"
	}

	url := fmt.Sprintf("https://%s%s", fqdn, healthPath)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("HTTP: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func printMonitorSummary(results []monitorAppResult, hasWarnings bool) {
	timestamp := time.Now().Format("15:04:05")

	healthy := 0
	unhealthy := 0
	down := 0
	for _, r := range results {
		s := r.baseStatus
		if s == "" {
			s = r.Status
		}
		switch {
		case s == "healthy":
			healthy++
		case s == "unreachable":
			down++
		default:
			unhealthy++
		}
	}

	fmt.Fprintf(os.Stderr, "[%s] ", timestamp)
	fmt.Fprint(os.Stderr, output.Green(fmt.Sprintf("%d healthy", healthy)))
	fmt.Fprint(os.Stderr, ", ")
	fmt.Fprint(os.Stderr, output.Yellow(fmt.Sprintf("%d unhealthy", unhealthy)))
	fmt.Fprint(os.Stderr, ", ")
	fmt.Fprint(os.Stderr, output.Red(fmt.Sprintf("%d down", down)))
	if hasWarnings {
		fmt.Fprint(os.Stderr, " "+output.Yellow("(warnings)"))
	}
	fmt.Fprintln(os.Stderr)

	for _, r := range results {
		if r.TLSDaysLeft > 0 && r.TLSDaysLeft <= 30 {
			fmt.Fprintf(os.Stderr, "[%s] %s: %s TLS cert expires in %d days\n",
				timestamp, output.Yellow("WARNING"), r.FQDN, r.TLSDaysLeft)
		}
		if r.Status == "unreachable" || r.Status == "tls-ok/http-down" {
			fmt.Fprintf(os.Stderr, "[%s] %s: %s is %s: %s\n",
				timestamp, output.Red("ERROR"), r.FQDN, r.Status, r.Error)
		}
	}
}

func writeMonitorStatus(checkedAt time.Time, results []monitorAppResult) {
	path := config.DefaultConfigDir + "/monitor.json"
	data, err := json.MarshalIndent(monitorResult{
		CheckedAt: checkedAt.Format(time.RFC3339),
		Apps:      results,
	}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[monitor] failed to marshal status: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[monitor] failed to write %s: %v\n", path, err)
	}
}
