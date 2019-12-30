package cmd

import (
	"github.com/urfave/cli/v2"
)

var App = cli.App{
	Name:  "utt",
	Usage: "UDP tunnel proxy.",
	Commands: []*cli.Command{
		newServerCmd(),
		newClientCmd(),
	},
}
