package output

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	white  = "\033[37m"
	bold   = "\033[1m"
	dim    = "\033[2m"
)

const (
	Checkmark = "✔"
	Crossmark = "✖"
	WarnIcon  = "⚠"
	InfoIcon  = "ℹ"
)

var UseColor = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

func style(code, s string) string {
	if !UseColor {
		return s
	}
	return code + s + reset
}

func Red(s string) string    { return style(red, s) }
func Green(s string) string  { return style(green, s) }
func Yellow(s string) string { return style(yellow, s) }
func Blue(s string) string   { return style(blue, s) }
func Bold(s string) string   { return style(bold, s) }
func Dim(s string) string    { return style(dim, s) }

func Success(msg string) { fmt.Println(Green(Checkmark + " " + msg)) }
func Successf(format string, args ...any) {
	fmt.Println(Green(fmt.Sprintf(Checkmark+" "+format, args...)))
}

func Info(msg string) { fmt.Println(Blue(InfoIcon + " " + msg)) }
func Infof(format string, args ...any) {
	fmt.Println(Blue(fmt.Sprintf(InfoIcon+" "+format, args...)))
}

func Warn(msg string) { fmt.Fprintln(os.Stderr, Yellow(WarnIcon+" "+msg)) }
func Warnf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, Yellow(fmt.Sprintf(WarnIcon+" "+format, args...)))
}

func Error(msg string) { fmt.Fprintln(os.Stderr, Red(Crossmark+" "+msg)) }

func HealthStatus(status string) string {
	switch {
	case status == "healthy" || strings.HasPrefix(status, "healthy"):
		return Green(status)
	case status == "down", status == "unreachable":
		return Red(status)
	case strings.HasPrefix(status, "unreachable"), strings.HasPrefix(status, "tls-ok/http-down"):
		return Red(status)
	case status == "-", status == "missing-snippet":
		return Dim(status)
	default:
		return Yellow(status)
	}
}

type Timer struct {
	start   time.Time
	label   string
	stopped bool
}

func StartTimer(label string) *Timer {
	if UseColor {
		fmt.Println(Bold(label + "..."))
	} else {
		fmt.Println(label + "...")
	}
	return &Timer{start: time.Now(), label: label}
}

func (t *Timer) Stop() {
	if t.stopped {
		return
	}
	t.stopped = true
	elapsed := time.Since(t.start).Round(100 * time.Millisecond)
	fmt.Printf("  %s (%v)\n", Dim("done"), elapsed)
}
