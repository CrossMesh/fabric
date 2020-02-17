package cmd

import (
	"context"
	"strings"
	"time"

	"git.uestc.cn/sunmxt/utt/control/rpc/pb"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func cmdNetworkSet(app *App, ctx *cli.Context) (err error) {
	if ctx.Args().Len() < 1 {
		return cmdError("network missing.")
	}
	if ctx.Args().Len() != 2 {
		return cmdError("cannot understand operation.")
	}
	netName := ctx.Args().Get(0)
	req := pb.SetNetworkRequest{
		Network: netName,
	}
	op := strings.ToLower(ctx.Args().Get(1))
	switch op {
	case "up":
		req.Start = true
	case "down":
		req.Start = false
	default:
		return cmdError("unknown operation \"%v\"", op)
	}
	conn, err := createControlClient(app.cfg.Control)
	if err != nil {
		return err
	}
	client := pb.NewNetworkManagmentClient(conn)

	// op
	var result *pb.Result
	cctx, canceled := context.WithTimeout(context.TODO(), time.Second*30)
	defer canceled()
	if result, err = client.SetNetwork(cctx, &req); err != nil {
		log.Error("control RPC got error: ", err)
		return err
	}
	if result == nil {
		return cmdError("control rpc got nil result")
	}
	if !result.Succeed {
		return cmdError("operation failed: %v", result.Message)
	}
	log.Info("operation succeeded: ", result.Message)

	return nil
}
