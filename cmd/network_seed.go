package cmd

import (
	"context"
	"net"
	"strings"
	"time"

	"git.uestc.cn/sunmxt/utt/control/rpc/pb"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
)

func cmdNetworkSeed(app *App, ctx *cli.Context) (err error) {
	if ctx.Args().Len() < 1 {
		log.Error("network missing.")
		return nil
	}
	if ctx.Args().Len() < 2 {
		log.Error("missing active endpoint.")
		return nil
	}
	netName := ctx.Args().Get(0)
	req := pb.SeedPeerRequest{
		Network: netName,
	}
	for _, ep := range ctx.Args().Slice()[1:] {
		parts := strings.SplitN(ep, ":", 2)
		if len(parts) < 2 {
			log.Error("endpoint with invalid format: ", ep)
			return nil
		}
		req.Endpoint = append(req.Endpoint, &pb.SeedPeerRequest_EndpointIdentity{
			EndpointType: parts[0],
			Endpoint:     parts[1],
		})
	}
	if app.cfg.Control == nil {
		log.Error("empty control RPC configuration. cannot connect to daemon.")
		return nil
	}
	// connect to daemon
	var conn *grpc.ClientConn
	if conn, err = grpc.Dial(app.cfg.Control.Endpoint, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(app.cfg.Control.Type, addr, timeout)
	}), grpc.WithInsecure()); err != nil {
		log.Error("connect to control RPC failure: ", err)
		return err
	}
	client := pb.NewNetworkManagmentClient(conn)

	// call seed peer.
	var result *pb.Result
	cctx, canceled := context.WithTimeout(context.TODO(), time.Second*30)
	defer canceled()
	if result, err = client.SeedPeer(cctx, &req); err != nil {
		log.Error("control rpc got error: ", err)
		return err
	}
	if result == nil {
		log.Error("control rpc got nil result")
		return nil
	}
	if !result.Succeed {
		log.Error("operation failed: ", result.Message)
	} else {
		log.Info("operation succeeded: ", result.Message)
	}
	return

}
