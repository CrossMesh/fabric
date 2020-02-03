package manager

import (
	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/config"
	logging "github.com/sirupsen/logrus"
)

type NetworkManager struct {
	router map[string]*Network

	log     *logging.Entry
	arbiter *arbit.Arbiter
}

func NewNetworkManager(arbiter *arbit.Arbiter, log *logging.Entry) (m *NetworkManager) {
	if log == nil {
		log = logging.WithField("module", "network_manager")
	}

	return &NetworkManager{
		router:  make(map[string]*Network),
		log:     log,
		arbiter: arbiter,
	}
}

func (n *NetworkManager) UpdateConfig(cfg *config.Daemon) (errs []error) {
	if cfg == nil {
		return nil
	}
	// update rpc control here.
	// update networks.
	networks := cfg.Net
	if networks == nil {
		networks = make(map[string]*config.Network)
	}
	// copy config references.
	for name, netCfg := range cfg.Net {
		networks[name] = netCfg
	}
	// remove missing.
	for name, net := range n.router {
		netCfg, hasNet := networks[name]
		if !hasNet {
			n.arbiter.Go(func() { net.Down() })
			delete(n.router, name)
			continue
		}
		// update
		if err := net.Reload(netCfg); err != nil {
			n.log.Error("reload network \"%v\" failure: ", err)
			errs = append(errs, err)
		}
		delete(networks, name)
	}
	// start new networks.
	for name, netCfg := range networks {
		net := newNetwork(n)
		n.router[name] = net
		if err := net.Reload(netCfg); err != nil {
			n.log.Error("reload network \"%v\" failure: ", err)
			errs = append(errs, err)
		}
	}
	return
}

func (n *NetworkManager) GetNetwork(name string) *Network {
	net, _ := n.router[name]
	return net
}
