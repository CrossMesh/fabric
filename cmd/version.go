package cmd

import (
	"fmt"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/urfave/cli/v2"
)

type versionPrinter struct{}

func (p versionPrinter) Println(args ...interface{}) { fmt.Println(args...) }

type versionPrintApp struct{}

func (a *versionPrintApp) AppName() string { return "version" }

func (a *versionPrintApp) LocalCommands() []*cli.Command {
	versionCmd := &cli.Command{
		Name:  "version",
		Usage: "print version info.",
		Action: func(ctx *cli.Context) (err error) {
			version.LogVersion(versionPrinter{})
			return nil
		},
	}
	return []*cli.Command{versionCmd}
}
