package cmd

import (
	"github.com/urfave/cli/v2"
)

func newNetworkCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "net",
		Usage: "network control.",
		Subcommands: []*cli.Command{
			//{
			//	Name:  "set",
			//	Usage: "start/stop network.",
			//	Action: func(ctx *cli.Context) (err error) {
			//		return cmdNetworkSet(app, ctx)
			//	},
			//},
			{
				Name:  "seed",
				Usage: "seed gossip peer.",
				Action: func(ctx *cli.Context) (err error) {
					return cmdNetworkSeed(app, ctx)
				},
			},
		},
	}
	return cmd
}
