package edgerouter

import "git.uestc.cn/sunmxt/utt/proto/pb"

func (r *EdgeRouter) Ping(msg *pb.Ping) (*pb.Ping, error) {
	return msg, nil
}

func (r *EdgeRouter) PeerExchange(peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	snapshot, err := r.route.Gossip().PBSnapshot()
	if err != nil {
		return nil, err
	}
	if peers != nil && peers.Peer != nil {
		r.routeArbiter.Go(func() { r.applyGossipPeers(peers.Peer) })
	}
	return &pb.PeerExchange{Peer: snapshot}, nil
}
