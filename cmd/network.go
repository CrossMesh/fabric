package cmd

import (
	"net"
	"time"

	"git.uestc.cn/sunmxt/utt/config"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
)

func createControlClient(cfg *config.ControlRPC) (conn *grpc.ClientConn, err error) {
	if cfg == nil {
		return nil, cmdError("empty control RPC configuration. cannot connect to daemon.")
	}
	if conn, err = grpc.Dial(cfg.Endpoint, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(cfg.Type, addr, timeout)
	}), grpc.WithInsecure()); err != nil {
		log.Error("connect to control RPC failure: ", err)
		return nil, err
	}
	return
}

func newNetworkCmd(app *App) *cli.Command {
	cmd := &cli.Command{
		Name:  "net",
		Usage: "network control.",
		Subcommands: []*cli.Command{
			{
				Name:  "set",
				Usage: "start/stop network.",
				Action: func(ctx *cli.Context) (err error) {
					return cmdNetworkSet(app, ctx)
				},
			},
			{
				Name:  "seed",
				Usage: "seed gossip peer.",
				Action: func(ctx *cli.Context) (err error) {
					return cmdNetworkSeed(app, ctx)
				},
			},
		},
	}
	return cmd
}
