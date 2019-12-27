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

func runClient(peer *config.Peer, remote, reverse, tunnel string) error {
	arbiter := arbiter.New(&log.Entry{
		Data: log.Fields{"module": "arbiter"},
	})

	arbiter.Go(func() {
		var (
			err                                 error
			remoteAddr, reverseAddr, tunnelAddr *net.UDPAddr
			tcpAddr                             *net.TCPAddr
			hasACL                              bool
			acl                                 *config.ACL
			connID                              uint
		)

		for arbiter.ShouldRun() {
			if err != nil {
				log.Info("retry in 5 second.")
				time.Sleep(time.Second * 5)
			}
			err = nil

			if remoteAddr, err = net.ResolveUDPAddr("udp", remote); err != nil {
				log.Error("remote address not resolved: ", err)
				continue
			}
			if reverseAddr, err = net.ResolveUDPAddr("udp", reverse); err != nil {
				log.Error("reverse address not resolved: ", err)
				continue
			}
			if tunnelAddr, err = net.ResolveUDPAddr("udp", tunnel); err != nil {
				log.Error("tunnel address not resolved: ", err)
				continue
			}
			if tcpAddr, err = net.ResolveTCPAddr("tcp", fmt.Sprintf("%v:%v", peer.Tunnel.Address, peer.Tunnel.Port)); err != nil {
				log.Error("tunnal address not resolved: ", err)
				continue
			}

			// ACL config
			aclName := fmt.Sprintf("%v:%v", remoteAddr.IP.To4().String(), remoteAddr.Port)
			log.Info("using ACL \"%v\"", aclName)
			if peer.ACL != nil {
				acl, hasACL = peer.ACL[aclName]
			}
			hasACL = acl != nil
			if !hasACL {
				log.Warn("acl rule \"%v\" not provided.", aclName)
			}

			c := relay.NewClient(tcpAddr, reverseAddr, remoteAddr, tunnelAddr)
			c.Log = &log.Entry{
				Data: log.Fields{
					"module":  "relay_client",
					"conn_id": connID,
				},
			}
			err = c.Do(arbiter)
		}
	})

	return arbiter.Arbit()
}

func newClientCmd() *cli.Command {
	configFile, remote, tunnel, reverse := "", "", "", ""
	cfg := &config.UUT{}

	cmd := &cli.Command{
		Name:    "client",
		Aliases: []string{"c"},
		Usage:   "run as client to connection to utt server endpoint.",
		Action: func(ctx *cli.Context) (err error) {
			if err = configor.Load(&cfg, configFile); err != nil {
				log.Error("failed to load configuration: ", err)
				return err
			}
			peer := pickPeerConfig(ctx, cfg)
			if peer == nil {
				return nil
			}
			return runClient(peer, remote, reverse, tunnel)
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
				Name:        "remote",
				Usage:       "Remote actual UDP endpoint.",
				Required:    true,
				Destination: &remote,
			},
			&cli.StringFlag{
				Name:        "reverse",
				Usage:       "UDP endpoint to receive reverse traffic.",
				Required:    true,
				Destination: &reverse,
			},
			&cli.StringFlag{
				Name:        "tunnel",
				Usage:       "UDP endpoint to receive tunnel traffic.",
				Required:    true,
				Destination: &tunnel,
			},
		}}

	return cmd
}
