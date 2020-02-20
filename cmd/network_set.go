package cmd

import (
	"context"
	"strings"
	"time"

	"git.uestc.cn/sunmxt/utt/control/rpc/pb"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
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

	var (
		conn   *grpc.ClientConn
		result *pb.Result
		cctx   context.Context
		cancel context.CancelFunc
	)
	retry := app.Retry
	shouldRetry := func() bool {
		if retry == -1 {
			return true
		}
		if retry > 0 {
			retry--
			return true
		}
		return false
	}

	for {
		conn, err = createControlClient(app.cfg.Control)
		if err != nil {
			err = cmdError("rpc connect failure: %v", err)
			if shouldRetry() {
				continue
			}
			return err
		}
		break
	}
	client := pb.NewNetworkManagmentClient(conn)

	// op
	for {
		if cancel != nil {
			cancel()
		}
		if err != nil {
			time.Sleep(time.Second * 3)
			err = nil
		}
		cctx, cancel = context.WithTimeout(context.TODO(), time.Second*30)

		if result, err = client.SetNetwork(cctx, &req); err != nil {
			err = cmdError("control RPC got error: %v", err)
			if shouldRetry() {
				continue
			}
			break
		}
		if result == nil {
			err = cmdError("control rpc got nil result")
			if shouldRetry() {
				continue
			}
			break
		}
		if !result.Succeed {
			err = cmdError("operation failed: %v", result.Message)
			if shouldRetry() {
				continue
			}
			break
		}
		log.Info("operation succeeded: ", result.Message)
		break
	}

	if cancel != nil {
		cancel()
	}
	return err
}
