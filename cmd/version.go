package cmd

import (
	"fmt"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/urfave/cli/v2"
)

type versionPrinter struct{}

func (p versionPrinter) Println(args ...interface{}) { fmt.Println(args...) }

func newVersionCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "version",
		Usage: "print version info.",
		Action: func(ctx *cli.Context) (err error) {
			version.LogVersion(versionPrinter{})
			return nil
		},
	}

	return cmd
}
