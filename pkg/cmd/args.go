package cmd

import (
	"git.uestc.cn/sunmxt/utt/pkg/config"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func pickNetConfig(ctx *cli.Context, cfg *config.Link) *config.Network {
	if ctx.Args().Len() < 1 {
		log.Error("network missing.")
	}
	if ctx.Args().Len() > 1 {
		log.Error("Too many networks specified.")
	}
	netName := ctx.Args().Get(0)
	net, ok := cfg.Net[netName]
	if !ok {
		log.Errorf("netWork \"%v\" not found.", netName)
		return nil
	}
	return net
}
