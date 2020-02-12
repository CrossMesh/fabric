package edgerouter

import (
	"errors"
	"io"
	"sync"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/proto"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
)

type forwardStatistics struct {
	read uint32
	sent uint32
}

var (
	ErrRelayNoBackend = errors.New("backend unavaliable")
)

func (r *EdgeRouter) receiveRemote(b backend.Backend, frame []byte, src string) {
	msgType, data := proto.UnpackProtocolMessageHeader(frame)
	peer, _ := r.route.Backward(data, backend.PeerBackendIdentity{
		Type:     b.Type(),
		Endpoint: src,
	})
	switch msgType {
	case proto.MsgTypeRPC:
		r.receiveRPCMessage(data, peer)
	case proto.MsgTypeRawFrame:
		r.backwardVTEP(data, peer)
	default:
		return
	}
}

func (r *EdgeRouter) backwardVTEP(frame []byte, peer route.MembershipPeer) {
	r.ifaceDevice.Write(frame)
}

func (r *EdgeRouter) forwardVTEPBackend(frame []byte, desp *route.PeerBackend) error {
	var b backend.Backend
	r.visitBackendsWithType(desp.Type, func(endpoint string, be backend.Backend) bool {
		b = be
		return false
	})
	if b == nil {
		return ErrRelayNoBackend
	}
	link, err := b.Connect(desp.Endpoint)
	if err != nil {
		if err == backend.ErrOperationCanceled {
			return nil
		}
		return nil
	}
	return link.Send(frame)
}

// VTEP to remote peers.
func (r *EdgeRouter) forwardVTEPPeer(frame []byte, peer route.MembershipPeer) error {
	if frame == nil || peer == nil {
		// drop nil frame and unknown destination.
		return nil
	}
	desp := peer.ActiveBackend()
	if desp.Type == pb.PeerBackend_UNKNOWN {
		// no active backend. drop.
		return nil
	}
	return r.forwardVTEPBackend(frame, desp)
}

func (r *EdgeRouter) forwardVTEP() {
	buf := make([]byte, 2048)
	for r.arbiter.ShouldRun() {
		// encode frame.
		readBuf := buf[proto.ProtocolMessageHeaderSize:]
		read, err := r.ifaceDevice.Read(readBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			r.log.Error("read link failure: ", err)
			continue
		}
		if read < 1 {
			continue
		}
		peers := r.route.Forward(readBuf)
		if len(peers) < 1 {
			continue
		}
		proto.PackProtocolMessageHeader(buf[:proto.ProtocolMessageHeaderSize], proto.MsgTypeRawFrame)
		packed := buf[:proto.ProtocolMessageHeaderSize+read]
		if len(peers) > 1 {
			// burst.
			var wg sync.WaitGroup

			for idx := range peers {
				wg.Add(1)
				go func() {
					r.forwardVTEPPeer(packed, peers[idx])
					wg.Done()
				}()
			}
			wg.Wait()
			continue
		}

		if err = r.forwardVTEPPeer(packed, peers[0]); err != nil && err != backend.ErrConnectionClosed {
			// disable unhealty backend.
		}
	}
}
