package edgerouter

import (
	"errors"
	"sync"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
)

type EdgeRouter struct {
	rpc *rpc.Stub

	lock         sync.RWMutex
	routeArbiter *arbit.Arbiter
	route        route.Router
	peerSelf     route.Peer

	forwardArbiter *arbit.Arbiter
	backends       sync.Map //map[route.PeerBackend]backend.Backend
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

func (a *EdgeRouter) Seed(peers ...route.Peer) {
	for idx := range peers {
		a.route.Gossip().Seed(peers[idx])
	}
}

func (r *EdgeRouter) visitBackends(visit func(route.PeerBackendIndex, backend.Backend) bool) {
	r.backends.Range(func(k, v interface{}) bool {
		index, isIndex := k.(route.PeerBackendIndex)
		if !isIndex {
			r.backends.Delete(k)
			return true
		}
		b, isBackend := k.(backend.Backend)
		if !isBackend {
			r.backends.Delete(k)
			return true
		}
		return visit(index, b)
	})
}

func (r *EdgeRouter) getBackend(index route.PeerBackendIndex) backend.Backend {
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
