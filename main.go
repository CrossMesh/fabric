package main

import (
	"os"

	"git.uestc.cn/sunmxt/utt/cmd"
)

func main() {
	if err := cmd.NewApp().Run(os.Args); err != nil {
		os.Exit(1)
	}
}
