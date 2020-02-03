package edgerouter

import (
	"errors"
	"sync"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
)

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
	backends       sync.Map //map[backend.PeerBackendIdentity]backend.Backend
	ifaceDevice    *water.Interface

	configID uint32
	cfg      *config.Network
	log      *logging.Entry
	arbiter  *arbit.Arbiter
}

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
	return a, nil
}

func (r *EdgeRouter) Membership() route.Membership {
	return r.membership
}

func (r *EdgeRouter) Mode() string {
	switch r.route.(type) {
	case *route.L2Router:
		return "ethernet"

	case *route.L3Router:
		return "overlay"
	}
	return "unknown"
}

func (r *EdgeRouter) visitBackends(visit func(backend.PeerBackendIdentity, backend.Backend) bool) {
	r.backends.Range(func(k, v interface{}) bool {
		index, isIndex := k.(backend.PeerBackendIdentity)
		if !isIndex {
			r.backends.Delete(k)
			return true
		}
		b, isBackend := v.(backend.Backend)
		if !isBackend {
			r.backends.Delete(k)
			return true
		}
		return visit(index, b)
	})
}

func (r *EdgeRouter) getBackend(index backend.PeerBackendIdentity) backend.Backend {
	v, hasBackend := r.backends.Load(index)
	if v == nil || !hasBackend {
		return nil
	}
	b, isBackend := v.(backend.Backend)
	if !isBackend {
		return nil
	}
	return b
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
