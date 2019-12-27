package cmd

import (
	"git.uestc.cn/sunmxt/utt/pkg/config"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func pickPeerConfig(ctx *cli.Context, cfg *config.UUT) *config.Peer {
	if ctx.Args().Len() < 1 {
		log.Error("peer config name missing.")
	}
	if ctx.Args().Len() > 1 {
		log.Error("Too many peer to connect.")
	}
	peerName := ctx.Args().Get(1)
	peer, ok := cfg.Peer[peerName]
	if !ok {
		log.Errorf("peer \"%v\" not found.", peerName)
		return nil
	}
	return peer
}
