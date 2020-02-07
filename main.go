package main

import (
	"os"

	"git.uestc.cn/sunmxt/utt/cmd"
)

func main() {
	cmd.NewApp().Run(os.Args)
}
