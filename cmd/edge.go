package cmd

import (
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/edgerouter"
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
			cfg := &config.Link{}
			if err = configor.Load(cfg, configFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}
			net := pickNetConfig(ctx, cfg)
			if net == nil {
				return nil
			}
			var app *edgerouter.EdgeRouter
			app, err = edgerouter.New()
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
