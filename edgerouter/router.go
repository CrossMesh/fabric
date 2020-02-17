package edgerouter

import (
	"errors"
	"sync"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
)

// EdgeRouter builds overlay network.
type EdgeRouter struct {
	rpc       *rpc.Stub
	rpcClient *rpc.Client
	rpcServer *rpc.Server

	lock         sync.RWMutex
	routeArbiter *arbit.Arbiter
	route        route.Router
	membership   route.Membership
	peerSelf     route.MembershipPeer

	forwardArbiter *arbit.Arbiter
	backends       sync.Map // map[pb.PeerBackend_BackendType]map[string]backend.Backend
	ifaceDevice    *water.Interface

	endpointFailures sync.Map // map[backend.PeerBackendIdentity]time.Time

	configID uint32
	cfg      *config.Network
	log      *logging.Entry
	arbiter  *arbit.Arbiter
}

// New creates a new EdgeRouter.
func New(arbiter *arbit.Arbiter) (a *EdgeRouter, err error) {
	if arbiter == nil {
		return nil, errors.New("arbiter missing")
	}
	a = &EdgeRouter{
		log:     logging.WithField("module", "edge_router"),
		arbiter: arbiter,
	}
	if err = a.initRPCStub(); err != nil {
		return nil, err
	}
	a.goCleanUp()
	return a, nil
}

func (r *EdgeRouter) goCleanUp() {
	r.arbiter.Go(func() {
		<-r.arbiter.Exit() // watch exit signal.

		r.lock.Lock()
		defer r.lock.Unlock()

		// terminate forwarding.
		if r.forwardArbiter != nil {
			r.forwardArbiter.Shutdown()
		}

		// close backend.
		r.visitBackends(func(id backend.PeerBackendIdentity, b backend.Backend) bool {
			b.Shutdown()
			r.deleteBackend(id)
			return true
		})

		// shutdown route decision.
		if r.routeArbiter != nil {
			r.routeArbiter.Shutdown()
		}

		// close interface.
		if r.ifaceDevice != nil {
			r.ifaceDevice.Close()
		}

		r.log.Info("edgerouter cleaned up.")
	})
}

// Membership gives current membership of edge router.
func (r *EdgeRouter) Membership() route.Membership {
	return r.membership
}

// Mode returns name of edge router working mode.
// values can be: ethernet, overlay.
func (r *EdgeRouter) Mode() string {
	switch r.route.(type) {
	case *route.L2Router:
		return "ethernet"

	case *route.L3Router:
		return "overlay"
	}
	return "unknown"
}

func visitBackendEndpointMap(byEndpoint *sync.Map, visit func(string, backend.Backend) bool) {
	byEndpoint.Range(func(k, v interface{}) bool {
		endpoint, isEndpoint := k.(string)
		if !isEndpoint {
			byEndpoint.Delete(k)
			return true
		}
		b, isBackend := v.(backend.Backend)
		if !isBackend {
			byEndpoint.Delete(k)
			return true
		}
		return visit(endpoint, b)
	})
}

func (r *EdgeRouter) visitBackendsWithType(ty pb.PeerBackend_BackendType, visit func(string, backend.Backend) bool) {
	raw, hasType := r.backends.Load(ty)
	if !hasType {
		return
	}
	byEndpoint, isMap := raw.(*sync.Map)
	if !isMap {
		return
	}
	visitBackendEndpointMap(byEndpoint, visit)
}

func (r *EdgeRouter) visitBackends(visit func(backend.PeerBackendIdentity, backend.Backend) bool) {
	r.backends.Range(func(k, v interface{}) (cont bool) {
		ty, isTy := k.(pb.PeerBackend_BackendType)
		if !isTy {
			r.backends.Delete(k)
			return true
		}
		byEndpoint, isMap := v.(*sync.Map)
		if !isMap || byEndpoint == nil {
			r.backends.Delete(k)
			return true
		}
		visitBackendEndpointMap(byEndpoint, func(endpoint string, b backend.Backend) bool {
			cont = visit(backend.PeerBackendIdentity{
				Type:     ty,
				Endpoint: endpoint,
			}, b)
			return cont
		})
		return
	})
}

func (r *EdgeRouter) getEndpointBackendMap(ty pb.PeerBackend_BackendType, create bool) (m *sync.Map) {
	rm, hasType := r.backends.Load(ty)
	if rm == nil || !hasType {
		if create {
			m = &sync.Map{}
			rm, hasType = r.backends.LoadOrStore(ty, m)
			if !hasType {
				return m
			}
		} else {
			return nil
		}
	}
	return rm.(*sync.Map) // let it crash.
}

func (r *EdgeRouter) getBackend(index backend.PeerBackendIdentity) backend.Backend {
	byEndpoint := r.getEndpointBackendMap(index.Type, false)
	if byEndpoint == nil {
		return nil
	}
	rb, hasBackend := byEndpoint.Load(index.Endpoint)
	if !hasBackend {
		return nil
	}
	b, isBackend := rb.(backend.Backend)
	if !isBackend {
		return nil
	}
	return b
}

func (r *EdgeRouter) deleteBackend(index backend.PeerBackendIdentity) {
	m := r.getEndpointBackendMap(index.Type, false)
	if m == nil {
		return
	}
	m.Delete(index.Endpoint)
}

func (r *EdgeRouter) storeBackend(index backend.PeerBackendIdentity, b backend.Backend) {
	m := r.getEndpointBackendMap(index.Type, true)
	if m == nil {
		return
	}
	m.Store(index.Endpoint, b)
}

// NewEmptyPeer return empty peer according to edge router mode..
func (r *EdgeRouter) NewEmptyPeer() (gossip.MembershipPeer, route.MembershipPeer) {
	switch mode := r.Mode(); mode {
	case "ethernet":
		p := &route.L2Peer{}
		return p, p
	case "overlay":
		p := &route.L3Peer{}
		return p, p
	default:
		// should not hit this.
		r.log.Errorf("EdgeRouter.NewPeer() got unknown mode %v.", mode)
	}
	return nil, nil
}
