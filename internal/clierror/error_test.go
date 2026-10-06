package clierror

import (
	"context"
	"errors"
	"testing"
)

func TestFromClassifiesValidation(t *testing.T) {
	got := From(errors.New("invalid subdomain"))
	if got.Code != CodeValidation || got.Category != "validation" || got.ExitStatus != 1 {
		t.Fatalf("unexpected error: %+v", got)
	}
}

func TestFromClassifiesTimeout(t *testing.T) {
	got := From(context.DeadlineExceeded)
	if got.Code != CodeTimeout || !got.Retryable || got.ExitStatus != 124 {
		t.Fatalf("unexpected timeout: %+v", got)
	}
}

func TestFromPreservesClassifiedExitStatus(t *testing.T) {
	got := From(&ClassifiedError{Code: CodeSSHUnavailable, Category: "transport", Retryable: true, ExitStatus: 69, Err: errors.New("connection refused")})
	if got.Code != CodeSSHUnavailable || got.ExitStatus != 69 || !got.Retryable {
		t.Fatalf("unexpected remote error: %+v", got)
	}
}
