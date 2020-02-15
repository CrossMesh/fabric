package cmd

import (
	"fmt"

	"git.uestc.cn/sunmxt/utt/config"
	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var App2 = cli.App{}

type App struct {
	*cli.App

	ConfigFile string
	cfg        *config.Daemon
}

func NewApp() (a *App) {
	a = &App{
		cfg: &config.Daemon{},
	}
	a.App = &cli.App{
		Name:  "utt",
		Usage: "Overlay network router, designed for connecting cloud network infrastructure",
		Commands: []*cli.Command{
			newEdgeCmd(a),
			newNetworkCmd(a),
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "config file",
				Required:    true,
				Destination: &a.ConfigFile,
			},
		},
		Before: func(ctx *cli.Context) (err error) {
			if err = configor.Load(a.cfg, a.ConfigFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}
			return nil
		},
	}
	return
}

func cmdError(format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	log.Error(err)
	return err
}
