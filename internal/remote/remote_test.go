package remote

import (
	"strings"
	"testing"
)

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\\''s'"},
		{"a b", "'a b'"},
		{"$HOME", "'$HOME'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellEscape(tt.input)
		if got != tt.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBuildRemoteCommand(t *testing.T) {
	r := &Remote{
		VitrinaPath: "/usr/local/bin/vitrina",
		UseSudo:     false,
	}

	cmd := buildRemoteCommand(r, "add", []string{"myapp", "3000"})
	expected := "'/usr/local/bin/vitrina' 'add' 'myapp' '3000'"
	if cmd != expected {
		t.Errorf("got %q, want %q", cmd, expected)
	}
}

func TestBuildRemoteCommandSudo(t *testing.T) {
	r := &Remote{
		VitrinaPath: "/tmp/vitrina",
		UseSudo:     true,
		User:        "deploy",
	}

	cmd := buildRemoteCommand(r, "list", nil)
	if !strings.HasPrefix(cmd, "sudo ") {
		t.Errorf("expected sudo prefix, got %q", cmd)
	}
	if !strings.Contains(cmd, "'/tmp/vitrina'") {
		t.Errorf("expected vitrina path, got %q", cmd)
	}
}

func TestBuildRemoteCommandNoSudoForRoot(t *testing.T) {
	r := &Remote{
		VitrinaPath: "/usr/local/bin/vitrina",
		UseSudo:     true,
		User:        "root",
	}

	cmd := buildRemoteCommand(r, "list", nil)
	if strings.HasPrefix(cmd, "sudo ") {
		t.Errorf("should not add sudo for root user, got %q", cmd)
	}
}

func TestBuildRemoteCommandEmptyArgs(t *testing.T) {
	r := &Remote{VitrinaPath: "/usr/local/bin/vitrina"}
	cmd := buildRemoteCommand(r, "list", nil)
	expected := "'/usr/local/bin/vitrina' 'list'"
	if cmd != expected {
		t.Errorf("got %q, want %q", cmd, expected)
	}
}

func TestBuildRemoteCommandSpaces(t *testing.T) {
	r := &Remote{VitrinaPath: "/usr/local/bin/vitrina"}
	cmd := buildRemoteCommand(r, "deploy", []string{"my app", "--branch", "feat/foo"})
	if !strings.Contains(cmd, "'my app'") {
		t.Errorf("expected quoted arg with spaces, got %q", cmd)
	}
	if !strings.Contains(cmd, "'feat/foo'") {
		t.Errorf("expected quoted arg with slash, got %q", cmd)
	}
}

func TestBuildRemoteCommandSingleQuote(t *testing.T) {
	r := &Remote{VitrinaPath: "/usr/local/bin/vitrina"}
	cmd := buildRemoteCommand(r, "add", []string{"it's working"})
	if !strings.Contains(cmd, `'it'\''s working'`) {
		t.Errorf("expected escaped single quote, got %q", cmd)
	}
}

func TestFilterArgsStripsRemoteFlag(t *testing.T) {
	raw := []string{"-r", "prod", "add", "myapp", "3000"}
	result := filterArgs(raw, "add")

	expected := []string{"myapp", "3000"}
	if len(result) != len(expected) {
		t.Fatalf("got %v, want %v", result, expected)
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestFilterArgsStripsLongRemoteFlag(t *testing.T) {
	raw := []string{"--remote", "prod", "add", "myapp"}
	result := filterArgs(raw, "add")

	expected := []string{"myapp"}
	if len(result) != len(expected) || result[0] != expected[0] {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestFilterArgsStripsRemoteEquals(t *testing.T) {
	raw := []string{"--remote=prod", "add", "myapp"}
	result := filterArgs(raw, "add")

	expected := []string{"myapp"}
	if len(result) != len(expected) || result[0] != expected[0] {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestFilterArgsStripsShortRemoteWithValue(t *testing.T) {
	raw := []string{"-rprod", "add", "myapp"}
	result := filterArgs(raw, "add")

	expected := []string{"myapp"}
	if len(result) != len(expected) || result[0] != expected[0] {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestFilterArgsStripsSubcommand(t *testing.T) {
	raw := []string{"deploy", "--branch", "main", "myapp", "url"}
	result := filterArgs(raw, "deploy")

	expected := []string{"--branch", "main", "myapp", "url"}
	if len(result) != len(expected) {
		t.Fatalf("got %v, want %v", result, expected)
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestFilterArgsNoDuplicate(t *testing.T) {
	raw := []string{"-r", "prod", "add", "myapp", "3000"}
	result := filterArgs(raw, "add")

	// After filtering -r/prod and skipping subcommand "add",
	// we should NOT have "add" in the result
	expected := []string{"myapp", "3000"}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %q, want %q", i, v, expected[i])
		}
	}
	if len(result) != 2 {
		t.Errorf("got %d args, want 2: %v", len(result), result)
	}
}

func TestFilterArgsPreservesFlags(t *testing.T) {
	raw := []string{"deploy", "--branch", "feat/test", "--tag", "v1.0", "myapp", "url"}
	result := filterArgs(raw, "deploy")

	expected := []string{"--branch", "feat/test", "--tag", "v1.0", "myapp", "url"}
	if len(result) != len(expected) {
		t.Fatalf("got %v, want %v", result, expected)
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %q, want %q", i, v, expected[i])
		}
	}
}

func TestFilterArgsSubcommandOnly(t *testing.T) {
	raw := []string{"list"}
	result := filterArgs(raw, "list")
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestApplyDefaults(t *testing.T) {
	r := &Remote{Host: "1.2.3.4"}
	r.ApplyDefaults()

	if r.User != "root" {
		t.Errorf("User: got %q, want root", r.User)
	}
	if r.Port != 22 {
		t.Errorf("Port: got %d, want 22", r.Port)
	}
	if r.VitrinaPath != "/usr/local/bin/vitrina" {
		t.Errorf("VitrinaPath: got %q, want /usr/local/bin/vitrina", r.VitrinaPath)
	}
}

func TestApplyDefaultsPreservesExplicitValues(t *testing.T) {
	r := &Remote{
		Host:        "1.2.3.4",
		User:        "deploy",
		Port:        2222,
		VitrinaPath: "/opt/vitrina",
	}
	r.ApplyDefaults()

	if r.User != "deploy" {
		t.Error("User was overwritten")
	}
	if r.Port != 2222 {
		t.Error("Port was overwritten")
	}
	if r.VitrinaPath != "/opt/vitrina" {
		t.Error("VitrinaPath was overwritten")
	}
}
