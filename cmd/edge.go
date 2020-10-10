package cmd

import (
	"context"
	"time"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/fabric/control"
	"github.com/crossmesh/fabric/control/rpc/pb"
	log "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
	"github.com/urfave/cli/v2"
)

func newEdgeReloadCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "reload",
		Usage: "reload config.",
		Action: func(ctx *cli.Context) (err error) {
			conn, err := createControlClient(app.cfg.Control)
			if err != nil {
				return err
			}
			req := pb.ReloadRequest{
				ConfigFilePath: app.ConfigFile,
			}
			client := pb.NewNetworkManagmentClient(conn)

			var result *pb.Result
			cctx, canceled := context.WithTimeout(context.TODO(), time.Second*30)
			defer canceled()
			if result, err = client.ReloadConfig(cctx, &req); err != nil {
				return cmdError("control rpc got error: %v", err)
			}
			if result == nil {
				return cmdError("control rpc got nil result")
			}
			if !result.Succeed {
				return cmdError("operation failed: %v", result.Message)
			}
			log.Info("operation succeeded: ", result.Message)

			return
		},
	}
	return cmd
}

func newEdgeCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "edge",
		Usage: "run as network peer.",
		Action: func(ctx *cli.Context) (err error) {
			version.LogVersion(nil)

			arbiter := arbit.New()
			arbiter.HookPreStop(func() {
				log.Info("shutting down...")
			})
			arbiter.HookStopped(func() {
				log.Info("exiting...")
			})
			defer arbiter.Shutdown()

			mgr := control.NewNetworkManager(arbiter, nil)
			if errs := mgr.UpdateConfig(app.cfg); errs != nil {
				log.Error("invalid config: ", errs)
				return nil
			}
			// legacy.
			for _, netName := range ctx.Args().Slice() {
				net := mgr.GetNetwork(netName)
				if net == nil {
					log.Error("network \"%v\" not found.", netName)
					continue
				}
				if err = net.Up(); err != nil {
					log.Error("network setup failure: ", err)
					continue
				}
			}
			return arbiter.Arbit()
		},
		Subcommands: []*cli.Command{
			newEdgeReloadCmd(app),
		},
		Before: func(ctx *cli.Context) (err error) {
			return app.loadConfig()
		},
	}

	return cmd
}
