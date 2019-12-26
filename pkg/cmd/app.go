package cmd

import (
	"github.com/urfave/cli/v2"
)

// utt server 121.48.165.58:4790 -in 127.0.0.1:3789 -out 121.48.165.58:4789
// utt client 121.48.165.58:4790 -in 127.0.0.1:4790 -out 121.48.165.58:4790

var App = cli.App{
	Name:  "utt",
	Usage: "UDP tunnel proxy.",
	Commands: []*cli.Command{
		newServerCmd(),
		newClientCmd(),
	},
}
