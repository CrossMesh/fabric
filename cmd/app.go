package cmd

import (
	"github.com/urfave/cli/v2"
)

var App = cli.App{
	Name:  "utt",
	Usage: "Overlay L2/L3 edge router",
	Commands: []*cli.Command{
		newEdgeCmd(),
	},
}
