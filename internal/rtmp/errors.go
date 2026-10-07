package rtmp

import (
	"errors"
	"io"
	"net"
	"syscall"
)

// AcceptError contains only fixed diagnostic labels. The underlying error may
// contain peer-controlled strings; it must never be logged directly.
type AcceptError struct {
	Stage  string
	Reason string
	cause  error
}

func (e *AcceptError) Error() string { return e.Stage + ": " + e.Reason }
func (e *AcceptError) Unwrap() error { return e.cause }

func acceptFailure(stage string, err error) *AcceptError {
	reason := "protocol_error"
	var netErr net.Error
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		reason = "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		reason = "connection_closed"
	}
	return &AcceptError{Stage: stage, Reason: reason, cause: err}
}
