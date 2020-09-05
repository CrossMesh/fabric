package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/crossmesh/fabric/control/rpc/pb"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func cmdNetworkSeed(app *App, ctx *cli.Context) error {
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
	conn, err := createControlClient(app.cfg.Control)
	if err != nil {
		return err
	}
	client := pb.NewNetworkManagmentClient(conn)

	// seed peer.
	var result *pb.Result
	cctx, canceled := context.WithTimeout(context.TODO(), time.Second*30)
	defer canceled()
	if result, err = client.SeedPeer(cctx, &req); err != nil {
		return cmdError("control rpc got error: ", err)
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
