package route

import (
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	logging "github.com/sirupsen/logrus"
)

type Router interface {
	Gossip() *GossipGroup

	Forward([]byte) []Peer
	Backward([]byte, PeerBackend) Peer
}

type BaseRouter struct {
	byPublish map[PeerBackend]Peer

	log *logging.Entry
}

func (r *BaseRouter) backendUpdated(p Peer, olds, news []PeerBackend) {
	for _, backend := range olds {
		delete(r.byPublish, backend)
	}
	for _, backend := range news {
		r.byPublish[backend] = p
	}
}

func (r *BaseRouter) append(v gossip.MembershipPeer) {
	peer, ok := v.(Peer)
	if !ok {
		r.log.Errorf("invalid gossip member for router to append: ", v)
		return
	}
	// map backends.
	for _, backend := range peer.Meta().backendByPriority {
		r.byPublish[backend.PeerBackend] = peer
	}
	// watch backend changes.
	peer.OnBackendUpdated(r.backendUpdated)
}

func (r *BaseRouter) remove(v gossip.MembershipPeer) {
	peer, ok := v.(Peer)
	if !ok {
		r.log.Errorf("invalid gossip member for router to remove: ", v)
		return
	}
	for _, backend := range peer.Meta().backendByPriority {
		delete(r.byPublish, backend.PeerBackend)
	}
}
