package route

import (
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

type Router interface {
	Forward([]byte) []MembershipPeer
	Backward([]byte, backend.PeerBackendIdentity) (MembershipPeer, bool)
	BackendPeer(backend backend.PeerBackendIdentity) MembershipPeer
	HotPeers(time.Duration) []MembershipPeer
}

type BaseRouter struct {
	hots      sync.Map
	byBackend sync.Map

	log *logging.Entry
}

type hotPeer struct {
	p       MembershipPeer
	lastHit time.Time
}

func (r *BaseRouter) hitPeer(p MembershipPeer) {
	if p == nil {
		return
	}
	hot := hotPeer{
		p:       p,
		lastHit: time.Now(),
	}
	r.hots.Store(p.Meta(), hot)
}

// BackendPeer maps backend.PeerBackendIdentity to MembershipPeer.
func (r *BaseRouter) BackendPeer(backend backend.PeerBackendIdentity) (p MembershipPeer) {
	v, ok := r.byBackend.Load(backend)
	if !ok {
		return nil
	}
	p, _ = v.(MembershipPeer)
	return
}

func (r *BaseRouter) HotPeers(last time.Duration) (peers []MembershipPeer) {
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
	// update active backends for hot peers.
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.hots.Range(func(k, v interface{}) bool {
			hot, isPeer := v.(MembershipPeer)
			if !isPeer {
				return true
			}
			hot.Meta().updateActiveBackend()
			return true
		})
	}, time.Second*2, 1)
}

func (r *BaseRouter) backendUpdated(p MembershipPeer, olds, news []backend.PeerBackendIdentity) {
	for _, backend := range olds {
		r.byBackend.Delete(backend)
	}
	for _, backend := range news {
		r.byBackend.Store(backend, p)
	}
}

func (r *BaseRouter) append(v MembershipPeer) {
	r.log.Infof("new peer up: %v", v.String())

	// map backends.
	for _, backend := range v.Meta().backendByPriority {
		r.byBackend.Store(backend.PeerBackendIdentity, v)
	}
	// watch backend changes.
	v.OnBackendUpdated(r.backendUpdated)
}

func (r *BaseRouter) remove(v MembershipPeer) {
	r.log.Infof("peer down: %v", v.String())

	r.hots.Delete(v.Meta())
	for _, backend := range v.Meta().backendByPriority {
		r.byBackend.Delete(backend.PeerBackendIdentity)
	}
}
