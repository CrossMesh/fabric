package cmd

import (
	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/control"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func newEdgeCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "edge",
		Usage: "run as network peer.",
		Action: func(ctx *cli.Context) (err error) {
			if ctx.Args().Len() < 1 {
				log.Error("network missing.")
				return nil
			}
			if ctx.Args().Len() > 1 {
				log.Error("Too many networks specified.")
				return nil
			}
			netName := ctx.Args().Get(0)

			arbiter := arbit.New(nil)
			arbiter.HookPreStop(func() {
				arbiter.Log().Info("shutting down...")
			})
			arbiter.HookStopped(func() {
				arbiter.Log().Info("exiting...")
			})
			defer arbiter.Shutdown()

			mgr := control.NewNetworkManager(arbiter, nil)
			if errs := mgr.UpdateConfig(app.cfg); errs != nil {
				log.Error("invalid config: ", errs)
				return nil
			}
			// legacy.
			net := mgr.GetNetwork(netName)
			if net == nil {
				log.Error("network \"%v\" not found.", netName)
				return nil
			}
			if err = net.Up(); err != nil {
				log.Error("network setup failure: ", err)
				return nil
			}
			return arbiter.Arbit()
		},
	}

	return cmd
}
