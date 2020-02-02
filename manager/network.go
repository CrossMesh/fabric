package manager

import (
	"sync"

	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/edgerouter"
)

type Network struct {
	lock sync.RWMutex

	mgr    *NetworkManager
	router *edgerouter.EdgeRouter
}

func newNetwork(mgr *NetworkManager) *Network {
	return &Network{mgr: mgr}
}

func (n *Network) Active() bool {
	return n.router == nil
}

func (n *Network) Down() error {
	return nil
}

func (n *Network) Up() error {
	return nil
}

func (n *Network) Reload(net *config.Network) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	if router == nil {
	}
	return nil
}
