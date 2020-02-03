package cmd

import (
	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/manager"
	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func newEdgeCmd() *cli.Command {
	configFile := ""

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

			cfg := &config.Daemon{}
			if err = configor.Load(cfg, configFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}

			arbiter := arbit.New(nil)
			arbiter.HookPreStop(func() {
				arbiter.Log().Info("shutting down...")
			})
			arbiter.HookStopped(func() {
				arbiter.Log().Info("exiting...")
			})
			defer arbiter.Shutdown()

			mgr := manager.NewNetworkManager(arbiter, nil)
			if errs := mgr.UpdateConfig(cfg); errs != nil {
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
