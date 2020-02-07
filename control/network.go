package control

import (
	"sync"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/edgerouter"
)

type Network struct {
	lock sync.RWMutex

	mgr     *NetworkManager
	router  *edgerouter.EdgeRouter
	arbiter *arbit.Arbiter
	cfg     *config.Network
}

func newNetwork(mgr *NetworkManager) *Network {
	return &Network{}
}

func (n *Network) Active() bool {
	return n.router != nil
}

func (n *Network) Down() error {
	n.mgr.arbiter.Go(func() {
		n.lock.Lock()
		defer n.lock.Unlock()

		if n.arbiter != nil {
			n.arbiter.Shutdown()
		}
		n.router = nil
	})
	return nil
}

func (n *Network) Up() (err error) {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.router == nil {
		if n.cfg == nil {
			return nil

		}
		n.arbiter = arbit.NewWithParent(n.arbiter, nil)
		if n.router, err = edgerouter.New(n.arbiter); err != nil {
			return err
		}
		n.router.ApplyConfig(n.cfg)
	}

	return nil
}

func (n *Network) Reload(net *config.Network) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	if net == nil {
		return nil
	}
	if n.router == nil {
		n.cfg = net
		return nil
	}
	if net.Equal(n.cfg) {
		return nil
	}
	n.cfg = net

	return n.router.ApplyConfig(net)
}

func (n *Network) Router() *edgerouter.EdgeRouter {
	return n.router
}
