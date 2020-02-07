package control

import (
	"context"

	"git.uestc.cn/sunmxt/utt/backend"
	cpb "git.uestc.cn/sunmxt/utt/control/rpc/pb"
	pb "git.uestc.cn/sunmxt/utt/proto/pb"
	logging "github.com/sirupsen/logrus"
)

var (
	resultInvalidRequest = &cpb.Result{Succeed: false, Message: "invalid request"}
	resultOK             = &cpb.Result{Succeed: true, Message: "ok"}
	resultNetworkIsDown  = &cpb.Result{Succeed: false, Message: "network is down"}
)

type controlRPCServer struct {
	*NetworkManager

	log *logging.Entry
}

func (s *controlRPCServer) SetNetwork(ctx context.Context, req *cpb.SetNetworkRequest) (*cpb.Result, error) {
	if req == nil {
		return resultInvalidRequest, nil
	}
	// find network.
	net := s.GetNetwork(req.Network)
	if net == nil {
		return &cpb.Result{Succeed: false, Message: "netwotk \"" + req.Network + "%v\" not found"}, nil
	}
	var err error
	if req.Start {
		err = net.Up()
	} else {
		err = net.Down()
	}
	if err != nil {
		return &cpb.Result{Succeed: false, Message: "operation failed: " + err.Error()}, nil
	}
	return resultOK, nil
}

func (s *controlRPCServer) SeedPeer(ctx context.Context, req *cpb.SeedPeerRequest) (*cpb.Result, error) {
	if req == nil {
		return resultInvalidRequest, nil
	}
	if len(req.Endpoint) < 1 {
		return &cpb.Result{Succeed: false, Message: "invalid request: peer has no avaliable endpoint"}, nil
	}
	// find network.
	net := s.GetNetwork(req.Network)
	if net == nil {
		return &cpb.Result{Succeed: false, Message: "netwotk \"" + req.Network + "%v\" not found"}, nil
	}
	router := net.Router()
	if !net.Active() || router == nil {
		return resultNetworkIsDown, nil
	}
	endpoints := make([]backend.PeerBackendIdentity, 0, len(req.Endpoint))
	for _, b := range req.Endpoint {
		ty, hasType := backend.TypeByName[b.EndpointType]
		if !hasType || ty == pb.PeerBackend_UNKNOWN {
			return &cpb.Result{Succeed: false, Message: "unsupported endpoint type \"" + b.EndpointType + "\""}, nil
		}
		endpoints = append(endpoints, backend.PeerBackendIdentity{
			Type:     ty,
			Endpoint: b.Endpoint,
		})
	}
	if err := net.Router().GossipSeedPeer(endpoints...); err != nil {
		return &cpb.Result{Succeed: false, Message: err.Error()}, err
	}
	return resultOK, nil
}
