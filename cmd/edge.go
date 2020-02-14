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
	}

	return cmd
}
