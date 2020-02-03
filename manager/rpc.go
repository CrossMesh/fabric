package manager

import (
	"context"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/manager/rpc/pb"
	"git.uestc.cn/sunmxt/utt/route"
	logging "github.com/sirupsen/logrus"
)

var (
	resultInvalidRequest = &pb.Result{Succeed: false, Message: "invalid request"}
	resultOK             = &pb.Result{Succeed: true, Message: "ok"}
	resultNetworkIsDown  = &pb.Result{Succeed: false, Message: "network is down"}
)

type controlRPCServer struct {
	*NetworkManager

	log *logging.Entry
}

func (s *controlRPCServer) SeedPeer(ctx context.Context, req *pb.SeedPeerRequest) (*pb.Result, error) {
	if req == nil {
		return resultInvalidRequest, nil
	}
	if len(req.Endpoint) < 1 {
		return &pb.Result{Succeed: false, Message: "invalid request: peer has no avaliable endpoint"}, nil
	}
	// find network.
	net := s.GetNetwork(req.Network)
	if net == nil {
		return &pb.Result{Succeed: false, Message: "netwotk \"" + req.Network + "%v\" not found"}, nil
	}
	router := net.Router()
	if !net.Active() || router == nil {
		return resultNetworkIsDown, nil
	}

	// get membership. allow only gossip membership.
	m, isGossip := router.Membership().(*route.GossipMemebership)
	if !isGossip {
		return &pb.Result{Succeed: false, Message: "not gossip membership"}, nil
	}
	if m == nil {
		return &pb.Result{Succeed: false, Message: "got nil membership interface"}, nil
	}

	// create new peer.
	forGossip, forMembership := router.NewEmptyPeer()
	if forGossip == nil || forMembership == nil {
		return &pb.Result{Succeed: false, Message: "cannot create new peer: got nil peer"}, nil
	}
	backends := make([]*route.PeerBackend, 0, len(req.Endpoint))
	for _, b := range req.Endpoint {
		backends = append(backends, &route.PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     b.EndpointType,
				Endpoint: b.Endpoint,
			},
		})
	}
	if !forMembership.Meta().Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
		tx.Backend(backends...)
		return true
	}) {
		return &pb.Result{Succeed: false, Message: "cannot apply endpoint hints."}, nil
	}
	m.Discover(forGossip)

	return resultOK, nil
}
