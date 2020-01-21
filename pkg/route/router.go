package route

import (
	"sync"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	logging "github.com/sirupsen/logrus"
)

type Router interface {
	Gossip() *GossipGroup

	Forward([]byte) []Peer
	Backward([]byte, PeerBackend) Peer
	BackendPeer(backend PeerBackend) Peer
	HotPeers(time.Duration) []Peer
}

type BaseRouter struct {
	hots      sync.Map
	byBackend sync.Map

	now time.Time
	log *logging.Entry
}

type hotPeer struct {
	p       Peer
	lastHit time.Time
}

func (r *BaseRouter) hitPeer(p Peer) {
	if p == nil {
		return
	}
	hot := hotPeer{
		p:       p,
		lastHit: r.now,
	}
	r.hots.Store(p.GossiperStub(), hot)
}

func (r *BaseRouter) BackendPeer(backend PeerBackend) (p Peer) {
	v, ok := r.byBackend.Load(backend)
	if !ok {
		return nil
	}
	p, _ = v.(Peer)
	return
}

func (r *BaseRouter) HotPeers(last time.Duration) (peers []Peer) {
	r.hots.Range(func(k, v interface{}) bool {
		hot, isPeer := v.(hotPeer)
		if !isPeer || time.Now().Add(-last).After(hot.lastHit) {
			r.hots.Delete(k)
		}
		peers = append(peers, hot.p)
		return true
	})
	return
}

func (r *BaseRouter) goTasks(arbiter *arbit.Arbiter) {
	// time ticking.
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.now = time.Now()
	}, time.Millisecond*50, 1)

	// update active backends for hot peers.
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.hots.Range(func(k, v interface{}) bool {
			hot, isPeer := v.(Peer)
			if !isPeer {
				return true
			}
			hot.Meta().updateActiveBackend()
			return true
		})
	}, time.Second*2, 1)
}

func (r *BaseRouter) backendUpdated(p Peer, olds, news []PeerBackend) {
	for _, backend := range olds {
		r.byBackend.Delete(backend)
	}
	for _, backend := range news {
		r.byBackend.Store(backend, p)
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
		r.byBackend.Store(backend.PeerBackend, peer)
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
	r.hots.Delete(peer.GossiperStub())
	for _, backend := range peer.Meta().backendByPriority {
		r.byBackend.Delete(backend.PeerBackend)
	}
}
