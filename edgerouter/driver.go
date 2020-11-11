package edgerouter

import (
	"io"

	"github.com/crossmesh/fabric/gossip"
	arbit "github.com/sunmxt/arbiter"
)

// ResourceFactory defines exported resources provided for OverlayDriver.
type ResourceFactory interface {
	OptionStore() OptionStore
	//Messager() Messager
}

// OverlayDriver provides overlay network supporting.
type OverlayDriver interface {
	Type() gossip.OverlayDriverType

	Init(*arbit.Arbiter, ResourceFactory) error
	HandleEvent(overlayEvent)
	UserCommand(info, err io.Writer, args []string) error
}
