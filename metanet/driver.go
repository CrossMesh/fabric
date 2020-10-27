package metanet

import (
	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/gossip"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

type resourceCollection interface {
	StoreTxn(writable bool) (StoreTxn, error)
	Log() *logging.Entry
	Arbiter() *arbit.Arbiter
}

// BackendManager manages a set of backends of type.
type BackendManager interface {
	Type() gossip.OverlayDriverType

	Init(resourceCollection) error

	// Activate enables endpoint.
	Activate(string) error
	// Deactivate disables endpoint.
	Deactivate(string) error
	// GetBackend gets backend with specific endpoint.
	GetBackend(string) backend.Backend

	// ListEndpoints reports all existing endpoints.
	ListEndpoints() []string
	// ListActiveEndpoints reports all active endpoints.
	ListActiveEndpoints() []string

	SetParams(string, args []string) error
}
