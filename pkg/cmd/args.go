package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func getTCPNetwork(ctx *cli.Context) (net string) {
	if ctx.Args().Len() < 1 {
		log.Error("TCP endpoint missing.")
	}
	if ctx.Args().Len() > 1 {
		log.Error("Too many TCP endpoints.")
	}
	return ctx.Args().Get(1)
}
