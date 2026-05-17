package mcp

import (
	"testing"
)

func TestIsValidSubdomain(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"blog", true},
		{"my-app", true},
		{"app123", true},
		{"a", true},
		{"", false},
		{"-hyphen", false},
		{"hyphen-", false},
		{"under_score", false},
		{"has space", false},
		{"a" + string(make([]byte, 63)) + "a", false},
	}

	for _, tt := range tests {
		got := isValidSubdomain(tt.input)
		if got != tt.valid {
			t.Errorf("isValidSubdomain(%q) = %v, want %v", tt.input, got, tt.valid)
		}
	}
}

func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("NewServer() returned nil")
	}
}