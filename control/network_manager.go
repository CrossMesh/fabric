package control

import (
	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/config"
	"github.com/jinzhu/configor"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

type coreDaemonConfig struct {
	Net   map[string]*config.Network `json:"link" yaml:"link"`
	Debug *bool                      `json:"debug" yaml:"debug"`
}

func (c *coreDaemonConfig) Equal(x *coreDaemonConfig) (e bool) {
	if len(c.Net) != len(x.Net) {
		return false
	}
	for name, net := range c.Net {
		xnet, has := x.Net[name]
		if !has {
			return false
		}
		if e = net.Equal(xnet); !e {
			return false
		}
	}
	return true
}

func (c *coreDaemonConfig) DebugEnabled() bool {
	if c.Debug == nil {
		return false
	}
	return *c.Debug
}

type NetworkManager struct {
	router map[string]*Network

	log     *logging.Entry
	arbiter *arbit.Arbiter
}

func NewNetworkManager(arbiter *arbit.Arbiter, log *logging.Entry) (m *NetworkManager) {
	if log == nil {
		log = logging.WithField("module", "network_manager")
	}
	m = &NetworkManager{
		router:  make(map[string]*Network),
		log:     log,
		arbiter: arbiter,
	}
	return
}

func (n *NetworkManager) UpdateConfigFromFile(path string) (errs common.Errors) {
	cfg := &coreDaemonConfig{}

	if err := configor.Load(cfg, path); err != nil {
		n.log.Error("failed to load configuration file: ", err)
		return common.Errors{err}
	}

	return n.UpdateConfig(cfg)
}

func (n *NetworkManager) UpdateConfig(cfg *coreDaemonConfig) (errs common.Errors) {
	if cfg == nil {
		return nil
	}

	if cfg.DebugEnabled() {
		logging.SetLevel(logging.TraceLevel)
	} else {
		logging.SetLevel(logging.InfoLevel)
	}

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
			errs.Trace(errs)
		}
		delete(networks, name)
	}
	// start new networks.
	for name, netCfg := range networks {
		net := newNetwork(n)
		if err := net.Reload(netCfg); err != nil {
			n.log.Error("start network \"%v\" failure: ", err)
			errs.Trace(errs)
			continue
		}
		n.router[name] = net
	}
	return
}

func (n *NetworkManager) GetNetwork(name string) *Network {
	net, _ := n.router[name]
	return net
}
