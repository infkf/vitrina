package clierror

import (
	"context"
	"errors"
	"strings"
)

const (
	CodeInternal       = "ERR_INTERNAL"
	CodeValidation     = "ERR_VALIDATION"
	CodeNotFound       = "ERR_NOT_FOUND"
	CodeConflict       = "ERR_STATE_CONFLICT"
	CodeTimeout        = "ERR_TIMEOUT"
	CodeInteraction    = "ERR_INTERACTION_REQUIRED"
	CodeSSHTimeout     = "ERR_SSH_TIMEOUT"
	CodeSSHAuth        = "ERR_SSH_AUTHENTICATION"
	CodeSSHUnavailable = "ERR_SSH_UNAVAILABLE"
	CodeRemoteCommand  = "ERR_REMOTE_COMMAND_FAILED"
	CodeDocker         = "ERR_DOCKER_FAILED"
	CodeGit            = "ERR_GIT_FAILED"
	CodeCaddy          = "ERR_CADDY_FAILED"
	CodePartial        = "ERR_PARTIAL_SUCCESS"
)

type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	Category   string `json:"category,omitempty"`
	ExitStatus int    `json:"exit_status"`
}

type ExitError struct {
	Code       string
	ExitStatus int
	Err        error
}

type ClassifiedError struct {
	Code       string
	Category   string
	Retryable  bool
	ExitStatus int
	Err        error
}

func (e *ClassifiedError) Error() string { return e.Err.Error() }
func (e *ClassifiedError) Unwrap() error { return e.Err }

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func From(err error) Error {
	if err == nil {
		return Error{Code: "OK"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Error{Code: CodeTimeout, Message: err.Error(), Retryable: true, Category: "timeout", ExitStatus: 124}
	}
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		status := classified.ExitStatus
		if status == 0 {
			status = 1
		}
		return Error{Code: classified.Code, Message: classified.Error(), Retryable: classified.Retryable, Category: classified.Category, ExitStatus: status}
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return Error{Code: exitErr.Code, Message: exitErr.Error(), ExitStatus: exitErr.ExitStatus, Category: categoryFor(exitErr.Code)}
	}
	message := err.Error()
	code := CodeInternal
	category := "internal"
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "invalid "), strings.Contains(lower, "must be "), strings.Contains(lower, "expected "):
		code, category = CodeValidation, "validation"
	case strings.Contains(lower, "not found"):
		code, category = CodeNotFound, "state"
	case strings.Contains(lower, "already "), strings.Contains(lower, "duplicate"):
		code, category = CodeConflict, "conflict"
	case strings.Contains(lower, "caddy"):
		code, category = CodeCaddy, "dependency"
	case strings.Contains(lower, "docker"):
		code, category = CodeDocker, "dependency"
	case strings.Contains(lower, "git") || strings.Contains(lower, "clone"):
		code, category = CodeGit, "dependency"
	}
	return Error{Code: code, Message: message, ExitStatus: 1, Category: category}
}

func categoryFor(code string) string {
	if strings.Contains(code, "SSH") || strings.Contains(code, "REMOTE") {
		return "transport"
	}
	if strings.Contains(code, "DOCKER") || strings.Contains(code, "GIT") || strings.Contains(code, "CADDY") {
		return "dependency"
	}
	return "command"
}
