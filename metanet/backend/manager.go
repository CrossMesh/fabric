package backend

import (
	"io"

	"github.com/crossmesh/fabric/common"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

// ResourceCollection defines resources required by manager.
type ResourceCollection interface {
	StoreTxn(writable bool) (common.StoreTxn, error)
	Log() *logging.Entry
	Arbiter() *arbit.Arbiter
}

// Manager manages a set of backends of type.
type Manager interface {
	Type() Type

	Init(ResourceCollection) error

	// Activate enables endpoint.
	Activate(string) error
	// Deactivate disables endpoint.
	Deactivate(string) error
	// GetBackend gets backend with specific endpoint.
	GetBackend(string) Backend

	// ListEndpoints reports all existing endpoints.
	ListEndpoints() []string
	// ListActiveEndpoints reports all active endpoints.
	ListActiveEndpoints() []string
}

// ParameterizedBackendManager accepts parameter configuration.
type ParameterizedBackendManager interface {
	SetParams(string, args []string) error
	ShowParams(io.Writer, ...string) error
}