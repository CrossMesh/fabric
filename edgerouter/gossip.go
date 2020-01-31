package edgerouter

import (
	"context"
	"errors"
	"time"

	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
)

func (r *EdgeRouter) goMembership() {
	switch v := r.membership.(type) {
	case *route.GossipMemebership:
		r.goGossip(v)
	}
}

func (r *EdgeRouter) goGossip(m *route.GossipMemebership) {
	log := r.log.WithField("type", "gossip")

	r.routeArbiter.TickGo(func(cancel func(), deadline time.Time) {
		term := m.NewTerm(1)
		ctx, _ := context.WithDeadline(r.routeArbiter.Context(), deadline)

		for idx := 0; idx < term.NumOfPeers(); idx++ {
			// exchange peer list with peers.
			peer, isPeer := term.Peer(idx).(route.MembershipPeer)
			if !isPeer {
				r.log.Warn("goGossip() got not route.MembershipPeer{}: ", term.Peer(idx))
				continue
			}
			snapshot, err := m.PBSnapshot()
			if err != nil {
				r.log.Error("goGossip() snapshot failure:", err)
				break
			}
			pb := &pb.PeerExchange{Peer: snapshot}

			// RPC
			r.routeArbiter.Go(func() {
				log, client := log.WithField("remote", peer.String()), r.RPCClient(peer)
				remote, err := client.GossipExchange(ctx, pb)
				if err != nil {
					if err != rpc.ErrRPCCanceled || r.routeArbiter.ShouldRun() {
						log.Warn("gossip with ", peer.String(), " failure: ", err)
					}
					return
				}
				if remote == nil {
					log.Warn("empty peer list from ", peer.String())
				}
				for _, err = range m.ApplyPBSnapshot(r.route, remote.Peer) {
					log.Warn("apply snapshot failure: ", err)
				}
			})
		}
	}, gossip.DefaultGossipPeriod, 1)
}

// GossipExchange is RPC method to gossip memberlist.
func (r *EdgeRouter) GossipExchange(peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	m, isGossip := r.membership.(*route.GossipMemebership)
	if !isGossip {
		return nil, errors.New("no gossip membership")
	}
	snapshot, err := m.PBSnapshot()
	if err != nil {
		return nil, err
	}
	if peers != nil && peers.Peer != nil {
		r.routeArbiter.Go(func() {
			for _, err := range m.ApplyPBSnapshot(r.route, peers.Peer) {
				r.log.Error("apply snapshot failure", err)
			}
		})
	}
	return &pb.PeerExchange{Peer: snapshot}, nil
}

// GossipExchange is port to make RPC call to GossipExchange method.
func (c *RPCClient) GossipExchange(ctx context.Context, peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	raw, err := c.Call(ctx, "GossipExchange", peers, c.send)
	if err != nil {
		return nil, err
	}
	resp, validType := raw.(*pb.PeerExchange)
	if !validType {
		return nil, ErrInvalidRPCMessageType
	}
	return resp, nil
}
