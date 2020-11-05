package backend

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownDestinationType = errors.New("Unknown destination type")
	ErrOperationCanceled      = errors.New("operation canceled")
	ErrConnectionDeined       = errors.New("connection deined")
	ErrConnectionClosed       = errors.New("connection closed")
	ErrBackendTypeUnknown     = errors.New("Unknown backend type")
	ErrInvalidBackendConfig   = errors.New("Unknown backend config")
)

// EndpointNotFoundError raised when endpoint cannot be found.
type EndpointNotFoundError struct {
	Endpoint
}

func (e *EndpointNotFoundError) Error() string {
	return fmt.Sprintf("endpoint %v cannot be found", e.Endpoint)
}
