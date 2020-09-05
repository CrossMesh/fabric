package main

import (
	"os"

	"github.com/crossmesh/fabric/cmd"
)

func main() {
	if err := cmd.NewApp().Run(os.Args); err != nil {
		os.Exit(1)
	}
}
