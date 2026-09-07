package colab

import (
	"fmt"
	"net/http"
)

type ErrorKind string

const (
	ErrorAuthRequired       ErrorKind = "auth-required"
	ErrorRuntimeUnavailable ErrorKind = "runtime-unavailable"
	ErrorKernelDisconnected ErrorKind = "kernel-disconnected"
	ErrorTransferFailed     ErrorKind = "transfer-failed"
	ErrorProtocolMismatch   ErrorKind = "protocol-mismatch"
)

type RemoteError struct {
	Kind       ErrorKind
	Operation  string
	StatusCode int
	Err        error
}

func (e *RemoteError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("Colab %s (%s)", e.Operation, e.Kind)
	}
	return fmt.Sprintf("Colab %s (%s): %v", e.Operation, e.Kind, e.Err)
}
func (e *RemoteError) Unwrap() error { return e.Err }

func ClassifyHTTPStatus(status int) ErrorKind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorAuthRequired
	case http.StatusPreconditionFailed, http.StatusServiceUnavailable, http.StatusTooManyRequests:
		return ErrorRuntimeUnavailable
	default:
		return ErrorTransferFailed
	}
}
