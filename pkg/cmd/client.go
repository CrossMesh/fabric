package cmd

import (
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"github.com/urfave/cli/v2"
)

func runClient(cfg *config.Tunnel) error {
	return nil
}

func newClientCmd() *cli.Command {
	cfg := &config.Tunnel{}
	cmd := &cli.Command{
		Name:    "client",
		Aliases: []string{"c"},
		Usage:   "run as client to connection to utt server endpoint.",
		Action: func(ctx *cli.Context) (err error) {
			if cfg.TCPNetwork = getTCPNetwork(ctx); cfg.TCPNetwork != "" {
				return
			}
			return runClient(cfg)
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "in",
				Usage:       "UDP listen endpoint to accept UDP traffic.",
				Required:    true,
				Destination: &cfg.UDPInNetwork,
			},
			&cli.StringFlag{
				Name:        "out",
				Usage:       "UDP endpoitn to relay traffic.",
				Required:    true,
				Destination: &cfg.UDPOutNetwork,
			},
		}}
	return cmd
}
