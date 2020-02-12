package edgerouter

import (
	"context"
	"errors"
	"strings"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
)

var (
	ErrNotGossipMembership    = errors.New("not gossip membership")
	ErrUnknownMode            = errors.New("unknown edge router mode")
	ErrGossipNoActiveEndpoint = errors.New("no active endpoint to gossip")
)

func (r *EdgeRouter) goMembership() {
	switch v := r.membership.(type) {
	case *route.GossipMembership:
		r.goGossip(v)
	}
}

func (r *EdgeRouter) newGossipPeer(snapshot *pb.Peer) (p gossip.MembershipPeer) {
	var snapshotPeer route.PBSnapshotPeer

	switch mode := r.Mode(); mode {
	case "ethernet":
		l2 := &route.L2Peer{}
		snapshotPeer, p = l2, l2
	case "overlay":
		l3 := &route.L3Peer{}
		snapshotPeer, p = l3, l3
	default:
		// should not hit this.
		r.log.Errorf("EdgeRouter.newGossipPeer() got unknown mode %v.", mode)
		return nil
	}

	snapshotPeer.ApplyPBSnapshot(snapshot)
	return
}

func (r *EdgeRouter) goGossip(m *route.GossipMembership) {
	log := r.log.WithField("type", "gossip")

	// seed myself.
	r.peerSelf.Meta().Self = true
	m.Discover(r.peerSelf.(gossip.MembershipPeer))

	r.routeArbiter.TickGo(func(cancel func(), deadline time.Time) {
		term := m.NewTerm(1)
		log.Infof("start gossip term %v.", term.ID)
		ctx, _ := context.WithDeadline(r.routeArbiter.Context(), deadline)

		peerDigest := make([]string, 0, term.NumOfPeers())
		for idx := 0; idx < term.NumOfPeers(); idx++ {
			// exchange peer list with peers.
			peer, isPeer := term.Peer(idx).(route.MembershipPeer)
			if !isPeer {
				r.log.Warn("goGossip() got not route.MembershipPeer{}: ", term.Peer(idx))
				continue
			}
			// log gossip peers.
			peerDigest = append(peerDigest, peer.String())

			snapshot, err := m.PBSnapshot()
			if err != nil {
				r.log.Error("goGossip() snapshot failure:", err)
				break
			}
			pbMsg := &pb.PeerExchange{Peer: snapshot}

			// trigger backend healthy check.
			r.goBackendHealthCheckOnce(peer)

			// Exchage memberlist
			if ab := peer.ActiveBackend(); ab == nil || ab.Type == pb.PeerBackend_UNKNOWN {
				// can not reach this peer.
				peer.Meta().GossiperStub().Tx(func(tx *gossip.PeerReleaseTx) bool {
					tx.ClaimDead()
					return true
				})
			} else {
				// Peer exchange RPC.
				r.routeArbiter.Go(func() {
					log, client := log.WithField("remote", peer.String()), r.RPCClient(peer)
					remote, err := client.GossipExchange(ctx, pbMsg)
					// A RPC failure doesn't mean death of peer.
					if err != nil {
						if err != rpc.ErrRPCCanceled || !r.routeArbiter.ShouldRun() {
							log.Warn("gossip with ", peer.String(), " failure: ", err)
						}
						log.Warn(err)
						return
					}
					// I know the peer is alive.
					peer.Meta().GossiperStub().Tx(func(tx *gossip.PeerReleaseTx) bool {
						tx.ClaimAlive()
						return true
					})
					if remote == nil {
						log.Warn("empty peer list from ", peer.String())
					}
					for _, err = range m.ApplyPBSnapshot(r.route, remote.Peer) {
						log.Warn("apply snapshot failure: ", err)
					}
				})
			}
		}
		if len(peerDigest) < 1 {
			log.Info("no member to gossip.")
		} else {
			log.Infof("gossip members: %v.", strings.Join(peerDigest, ","))
		}
		m.Clean(time.Now())

	}, gossip.DefaultGossipPeriod, 1)
}

func (r *EdgeRouter) GossipSeedPeer(bs ...backend.PeerBackendIdentity) error {
	if len(bs) < 1 {
		return ErrGossipNoActiveEndpoint
	}

	// get membership. allow only gossip membership.
	m, isGossip := r.membership.(*route.GossipMembership)
	if !isGossip || m == nil {
		return ErrNotGossipMembership
	}

	backends := make([]*route.PeerBackend, 0, len(bs))
	for _, b := range bs {
		backends = append(backends, &route.PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     b.Type,
				Endpoint: b.Endpoint,
			},
		})
	}

	// create new peer.
	switch mode := r.Mode(); mode {
	case "ethernet":
		l2 := &route.L2Peer{}
		l2.Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
			tx.Backend(backends...)
			return true
		})
		l2.Reset()
		m.Discover(l2)
	case "overlay":
		l3 := &route.L3Peer{}
		l3.Tx(func(p route.MembershipPeer, tx *route.L3PeerReleaseTx) bool {
			tx.Backend(backends...)
			return true
		})
		l3.Reset()
		m.Discover(l3)
	default:
		// should not hit this.
		r.log.Errorf("EdgeRouter.newGossipPeer() got unknown mode %v.", mode)
		return ErrUnknownMode
	}

	return nil
}

// GossipExchange is RPC method to gossip memberlist.
func (r *EdgeRouter) GossipExchange(peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	m, isGossip := r.membership.(*route.GossipMembership)
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
