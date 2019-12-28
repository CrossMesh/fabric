package cmd

import (
	"fmt"
	"net"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/relay"
	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func runServer(peer *config.Peer) error {
	arbiter := arbiter.New(&log.Entry{
		Data: log.Fields{"module": "arbiter"},
	})

	arbiter.Go(func() {
		var (
			err      error
			bindAddr *net.TCPAddr
			connID   uint
		)

		for arbiter.ShouldRun() {
			if err != nil {
				log.Info("retry in 5 second.")
				time.Sleep(time.Second * 5)
			}
			err = nil

			if bindAddr, err = net.ResolveTCPAddr("tcp", fmt.Sprintf("%v:%v", peer.Tunnel.Address, peer.Tunnel.Port)); err != nil {
				log.Error("tunnal address not resolved: ", err)
				continue
			}
			s := relay.NewServer(bindAddr, peer.ACL)
			s.Log = &log.Entry{
				Data: log.Fields{
					"module":  "relay_server",
					"conn_id": connID,
				},
			}
			err = s.Do(arbiter)
		}
	})

	return arbiter.Arbit()
}

func newServerCmd() *cli.Command {
	configFile, bind := "", ""

	cmd := &cli.Command{
		Name:    "server",
		Aliases: []string{"s"},
		Usage:   "run as server to wait for connection.",
		Action: func(ctx *cli.Context) (err error) {
			cfg := &config.UUT{}
			if err = configor.Load(&cfg, configFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}
			peer := pickPeerConfig(ctx, cfg)
			if peer == nil {
				return nil
			}
			if bindAddr, err := net.ResolveTCPAddr("tcp", bind); err != nil {
				log.Errorf("cannot resolve bind endpoint: %v", bind)
				return err
			} else {
				peer.Tunnel.Address = bindAddr.IP.To4().String()
				if bindAddr.Port > 0 && uint16(bindAddr.Port) <= uint16(0xFFFF) {
					peer.Tunnel.Port = uint16(bindAddr.Port)
				}
			}
			// default bind port
			if peer.Tunnel.Port == 0 {
				peer.Tunnel.Port = 4790
			}
			return runServer(peer)
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "config file",
				Required:    true,
				Destination: &configFile,
			},
			&cli.StringFlag{
				Name:        "bind",
				Usage:       "network endpoint to bind.",
				Required:    true,
				Destination: &bind,
			},
		}}

	return cmd
}
