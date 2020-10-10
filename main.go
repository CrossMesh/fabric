package main

import (
	"os"

	"github.com/crossmesh/fabric/cmd"
	"github.com/crossmesh/fabric/cmd/version"
)

func main() {
	app := cmd.NewApp()
	app.Version = version.Version + "-" + version.Revision
	app.Name = "crossmesh"

	if err := app.Run(os.Args); err != nil {
		os.Exit(1)
	}
}
