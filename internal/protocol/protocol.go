package protocol

import "time"

// Envelope is the stable stdout contract for every non-streaming invocation.
type Envelope struct {
	OK          bool      `json:"ok"`
	Operation   string    `json:"operation,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Data        any       `json:"data,omitempty"`
	Events      []any     `json:"events"`
	Error       any       `json:"error"`
}
