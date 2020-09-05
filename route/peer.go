package route

import (
	"github.com/crossmesh/fabric/backend"
	pbp "github.com/crossmesh/fabric/proto/pb"
)

const (
	Ethernet = 1
	Overlay  = 2
)

var PeerTypeName map[uint32]string = map[uint32]string{
	Ethernet: "ethernet",
	Overlay:  "overlay",
}

type MembershipPeer interface {
	Meta() *PeerMeta
	String() string
	OnBackendUpdated(func(MembershipPeer, []backend.PeerBackendIdentity, []backend.PeerBackendIdentity))
	ActiveBackend() *PeerBackend
	Backends() []*PeerBackend
}

type PBSnapshotPeer interface {
	PBSnapshot() (*pbp.Peer, error)
	ApplyPBSnapshot(*pbp.Peer) error
}

type Membership interface {
	MembershipListner
	MembershipVistor
}

type MembershipListner interface {
	OnAppend(func(MembershipPeer))
	OnRemove(func(MembershipPeer))
}

type MembershipVistor interface {
	Range(func(MembershipPeer) bool)
}
