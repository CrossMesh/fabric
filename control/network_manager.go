package control

import (
	"net"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/control/rpc/pb"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type NetworkManager struct {
	router map[string]*Network

	log     *logging.Entry
	arbiter *arbit.Arbiter

	controlConfig    config.ControlRPC
	controlListener  net.Listener
	controlRPCServer *grpc.Server
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

func (n *NetworkManager) ApplyControlRPCConfig(cfg *config.ControlRPC) (err error) {
	// update rpc control here.
	if !n.controlConfig.Equal(cfg) {
		if n.controlListener != nil {
			if err = n.controlListener.Close(); err != nil {
				n.log.Error("close control RPC failure: ", err)
				return err
			} else {
				n.log.Info("control RPC closed.")
				n.controlListener = nil
			}
		}

		if n.controlListener == nil && cfg != nil {
			if n.controlListener, err = net.Listen(cfg.Type, cfg.Endpoint); err != nil {
				n.log.Error("control RPC listen failure: ", err)
				n.controlListener = nil
			} else {
				n.log.Infof("control RPC listening to %v:%v.", cfg.Type, cfg.Endpoint)
				n.controlConfig = *cfg
			}
		}

		// start grpc server.
		if n.controlListener != nil {
			rpcServer := &controlRPCServer{
				NetworkManager: n,
				log:            n.log.WithField("type", "control_rpc"),
			}
			n.controlRPCServer = grpc.NewServer()
			pb.RegisterNetworkManagmentServer(n.controlRPCServer, rpcServer)
			n.arbiter.Go(func() {
				if err := n.controlRPCServer.Serve(n.controlListener); err != nil {
					n.log.Error("grpc.Server.Serve() failure: ", err)
				}
			})
			// clean up on exit.
			n.arbiter.Go(func() {
				<-n.arbiter.Exit()
				listener, rpcServer := n.controlListener, n.controlRPCServer
				if rpcServer != nil {
					rpcServer.Stop()
				}
				if listener != nil {
					listener.Close()
				}
			})
		}
	}

	return
}

func (n *NetworkManager) UpdateConfig(cfg *config.Daemon) (errs []error) {
	if cfg == nil {
		return nil
	}

	if err := n.ApplyControlRPCConfig(cfg.Control); err != nil {
		errs = append(errs, err)
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
			errs = append(errs, err)
		}
		delete(networks, name)
	}
	// start new networks.
	for name, netCfg := range networks {
		net := newNetwork(n)
		if err := net.Reload(netCfg); err != nil {
			n.log.Error("start network \"%v\" failure: ", err)
			errs = append(errs, err)
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
