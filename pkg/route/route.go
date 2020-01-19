package route

import (
	"git.uestc.cn/sunmxt/utt/pkg/backend"
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	pbp "git.uestc.cn/sunmxt/utt/pkg/proto/pb"
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
	gossip.Peer

	String() string
	PBSnapshot() (*pbp.Peer, error)
	Tx(func(Peer, PeerReleaseTx) bool)
}

type PeerReleaseTx interface {
	Backend(...backend.Backend)
	Region(string)
	Version(uint64)
}
