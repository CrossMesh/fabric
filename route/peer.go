package route

import (
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/gossip"
	pbp "git.uestc.cn/sunmxt/utt/proto/pb"
)

const (
	Ethernet = 1
	Overlay  = 2
)

var PeerTypeName map[uint32]string = map[uint32]string{
	Ethernet: "ethernet",
	Overlay:  "overlay",
}

type Peer interface {
	gossip.MembershipPeer

	Meta() *PeerMeta
	PBSnapshot() (*pbp.Peer, error)
	ApplyPBSnapshot(*pbp.Peer) error
	OnBackendUpdated(func(Peer, []backend.PeerBackendIdentity, []backend.PeerBackendIdentity))
	ActiveBackend() *PeerBackend
}

type MembershipPeer interface {
	Meta() *PeerMeta
	String() string
	OnBackendUpdated(func(Peer, []backend.PeerBackendIdentity, []backend.PeerBackendIdentity))
	ActiveBackend() *PeerBackend
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
