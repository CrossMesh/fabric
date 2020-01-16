package cmd

import (
	"fmt"

	arbit "git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/backend"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	"git.uestc.cn/sunmxt/utt/pkg/route"
	"git.uestc.cn/sunmxt/utt/pkg/rpc"
	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
	"github.com/urfave/cli/v2"
)

type EdgeRouterApp struct {
	rpc      *rpc.Stub
	peers    *route.PeerSet
	peerSelf *route.Peer

	ifaceDevice *water.Interface
}

func NewEdgeRouterApp() (a *EdgeRouterApp, err error) {
	a = &EdgeRouterApp{
		peers: route.NewPeerSet(logging.WithField("module", "peer_set")),
	}
	if err = a.initRPCStub(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *EdgeRouterApp) Ping(msg *pb.Ping) (*pb.Ping, error) {
	return msg, nil
}

func (a *EdgeRouterApp) PeerExchange(peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	return nil, nil
}

func (a *EdgeRouterApp) initRPCStub() (err error) {
	if a.rpc == nil {
		a.rpc = rpc.NewStub(logging.WithField("module", "rpc_stub"))
	}
	fns := []interface{}{
		a.PeerExchange,
		a.Ping,
	}
	for _, fn := range fns {
		if err = a.rpc.Register(fn); err != nil {
			return err
		}
	}
	return nil
}

func (a *EdgeRouterApp) Do(net *config.Network) (err error) {
	if net.Iface == "" {
		err = fmt.Errorf("interface name is empty")
		log.Error(err, ".")
		return err
	}
	if len(net.Backend) < 1 {
		err = fmt.Errorf("no backend configured")
		log.Error(err, ".")
		return err
	}

	// create tun/tap.
	deviceConfig := water.Config{
		PlatformSpecificParams: water.PlatformSpecificParams{
			Name: net.Iface,
		},
	}
	switch net.Mode {
	case "ethernet":
		deviceConfig.DeviceType = water.TAP
	case "overlay":
		deviceConfig.DeviceType = water.TUN
	default:
		err = fmt.Errorf("unknown network mode: %v", net.Mode)
		log.Error(err)
		return err
	}
	if a.ifaceDevice, err = water.New(deviceConfig); err != nil {
		log.Error("cannot create network device: ", err)
		return err
	}

	arbiterLog := logging.WithField("module", "arbiter")
	arbiter := arbit.New(arbiterLog)
	defer arbiter.Shutdown()

	// create backend.
	var backends []backend.Backend
	if backends, err = a.createBackends(arbiter, net.Backend); err != nil {
		log.Error("create backends failure: ", err)
		return err
	}
	// watch backend packets.
	// create peer self.
	if a.peerSelf, err = route.NewPeer(arbiter, net.Region, backends...); err != nil {
		log.Error("create peers failure: ", err)
		return err
	}

	arbiter.HookPreStop(func() {
		arbiterLog.Info("shutting down...")
	})
	arbiter.HookStopped(func() {
		arbiterLog.Info("exiting...")
	})

	return arbiter.Arbit()
}

func (a *EdgeRouterApp) createBackends(arbiter *arbit.Arbiter, cfgs []*config.Backend) (backends []backend.Backend, err error) {
	backends = make([]backend.Backend, 0, len(cfgs))
	defer func() {
		if err != nil {
			for _, backend := range backends {
				backend.Shutdown()
			}
		}
	}()
	for _, cfg := range cfgs {
		created, err := backend.CreateBackend(cfg.Type, arbiter, nil, cfg)
		if err != nil {
			return nil, err
		}
		backends = append(backends, created)
	}
	return backends, nil
}

func newEdgeCmd() *cli.Command {
	configFile := ""

	cmd := &cli.Command{
		Name:  "edge",
		Usage: "run as network peer.",
		Action: func(ctx *cli.Context) (err error) {
			cfg := &config.Link{}
			if err = configor.Load(cfg, configFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}
			net := pickNetConfig(ctx, cfg)
			if net == nil {
				return nil
			}
			var app *EdgeRouterApp
			app, err = NewEdgeRouterApp()
			if err != nil {
				log.Error("create router failure: ", err)
				return err
			}
			return app.Do(net)
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "config file",
				Required:    true,
				Destination: &configFile,
			},
		}}

	return cmd
}
