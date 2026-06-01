package output

import (
	"os"
	"testing"
)

func TestHealthStatus(t *testing.T) {
	UseColor = false
	tests := []struct {
		status string
		want   string
	}{
		{"healthy", "healthy"},
		{"healthy (TLS 30d)", "healthy (TLS 30d)"},
		{"down", "down"},
		{"unreachable", "unreachable"},
		{"unreachable (connection refused)", "unreachable (connection refused)"},
		{"tls-ok/http-down", "tls-ok/http-down"},
		{"-", "-"},
		{"unhealthy (503)", "unhealthy (503)"},
		{"reachable (tcp)", "reachable (tcp)"},
		{"missing-snippet", "missing-snippet"},
	}
	for _, tt := range tests {
		got := HealthStatus(tt.status)
		if got != tt.want {
			t.Errorf("HealthStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestColorsDisabled(t *testing.T) {
	UseColor = false
	if Red("err") != "err" {
		t.Error("expected plain text when UseColor is false")
	}
	if Green("ok") != "ok" {
		t.Error("expected plain text when UseColor is false")
	}
	if Yellow("warn") != "warn" {
		t.Error("expected plain text when UseColor is false")
	}
}

func TestColorsDisabledByEnv(t *testing.T) {
	oldNoColor := os.Getenv("NO_COLOR")
	os.Setenv("NO_COLOR", "1")
	if UseColor {
		t.Error("expected UseColor to be false when NO_COLOR=1")
	}
	os.Setenv("NO_COLOR", oldNoColor)
	UseColor = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}
