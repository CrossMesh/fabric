package cmd

import (
	"net"

	"github.com/crossmesh/fabric/cmd/pb"
	"google.golang.org/grpc"
)

func (a *CrossmeshApplication) reconfigurateControlRPC(cfg *controlRPCConfig) (err error) {
	if a.control.config.Equal(cfg) {
		return nil
	}

	for cfg != nil {

		if a.control.listener != nil {
			if err = a.control.listener.Close(); err != nil {
				a.log.Errorf("close control RPC failure. (err = \"%v\")", err)
				return err
			}
			a.log.Info("control RPC closed.")
			a.control.listener = nil
		}

		if a.control.listener == nil && cfg != nil {
			if a.control.listener, err = net.Listen(cfg.Type, cfg.Endpoint); err != nil {
				a.log.Errorf("control RPC listen failure. (err = \"%v\")", err)
				a.control.listener = nil

				cfg = a.control.config
				continue
			} else {
				a.log.Infof("control RPC listening to %v:%v.", cfg.Type, cfg.Endpoint)
				a.control.config = cfg
			}
		}

		// start grpc server.
		if a.control.listener != nil {
			a.control.rpcServer = grpc.NewServer()

			pb.RegisterDaemonControlServer(a.control.rpcServer, a)
			a.arbiters.control.Go(func() {
				if err = a.control.rpcServer.Serve(a.control.listener); err != nil {
					a.log.Errorf("grpc.Server.Serve() failure. (err = \"\")", err)
				}

				a.control.listener.Close()
				a.control.listener = nil
			})
			if err != nil {
				cfg = a.control.config // fallback
				continue
			}

			a.arbiters.control.Go(func() {
				<-a.arbiters.control.Exit()
				rpcServer := a.control.rpcServer
				if rpcServer != nil {
					rpcServer.Stop()
				}
			})
		}

		break
	}

	if a.control.config != cfg {
		a.control.config = cfg
	}

	return err
}
