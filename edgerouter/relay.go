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

func (r *EdgeRouter) receiveRemote(backend backend.Backend, frame []byte, src string) {
	msgType, data := proto.UnpackProtocolMessageHeader(frame)
	peer := r.route.Backward(data, route.PeerBackend{
		PeerBackendIndex: route.PeerBackendIndex{
			Type:     backend.Type(),
			Endpoint: src,
		},
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

func (r *EdgeRouter) backwardVTEP(frame []byte, peer route.Peer) {
	r.ifaceDevice.Write(frame)
}

// VTEP to remote peers.
func (r *EdgeRouter) forwardVTEPPeer(frame []byte, peer route.Peer) error {
	if frame == nil || peer == nil {
		return nil
	}
	desp := peer.ActiveBackend()
	if desp.Type == pb.PeerBackend_UNKNOWN {
		// no active backend.
		return nil
	}
	v, hasBackend := r.backends.Load(desp.Type)
	if !hasBackend || v == nil {
		return ErrRelayNoBackend
	}
	b, isBackend := v.(backend.Backend)
	if !isBackend {
		return ErrRelayNoBackend
	}
	link, err := b.Connect(desp.Endpoint)
	if err != nil {
		if err == backend.ErrOperationCanceled {
			return nil
		}
	}
	return link.Send(frame)
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
