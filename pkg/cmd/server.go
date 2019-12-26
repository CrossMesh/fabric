package cmd

import (
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"github.com/urfave/cli/v2"
)

func runServer(cfg *config.Tunnel) error {
	return nil
}

func newServerCmd() *cli.Command {
	cfg := &config.Tunnel{}
	cmd := &cli.Command{
		Name:    "server",
		Aliases: []string{"s"},
		Usage:   "run as server to wait for connection.",
		Action: func(ctx *cli.Context) (err error) {
			if cfg.TCPNetwork = getTCPNetwork(ctx); cfg.TCPNetwork != "" {
				return
			}
			return runServer(cfg)
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
