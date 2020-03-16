package edgerouter

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/proto"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
	"github.com/songgao/water"
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
	lease, err := r.vtep.QueueLease()
	if err != nil {
		r.log.Error("cannot acquire queue lease: ", err)
		return
	}
	if lease == nil {
		return
	}
	err = lease.Tx(func(rw *water.Interface) error {
		_, err := rw.Write(frame)
		return err
	})
	if err != nil && err != ErrVTEPQueueRevoke {
		r.log.Error("write VTEP failure: ", err)
	}
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

func (r *EdgeRouter) goForwardVTEP() {
	buf := make([]byte, 2048)

	r.forwardArbiter.Go(func() {
		var (
			lease        *vtepQueueLease
			err, readErr error
			read         int
		)

		for r.forwardArbiter.ShouldRun() {
			// acquire queue lease.
			for r.forwardArbiter.ShouldRun() {
				newLease, err := r.vtep.QueueLease()
				if err != nil {
					r.log.Error("cannot acquire queue lease: ", err)
					time.Sleep(time.Second * 5)
					continue
				}
				if newLease == nil {
					r.log.Error("cannot acquire queue lease: got nil lease.")
					time.Sleep(time.Second * 5)
					continue
				}
				lease = newLease
				break
			}

			if readErr != nil {
				// force synchronize system NIC configuration.
				if err = r.vtep.SynchronizeSystemConfig(); err != nil {
					r.log.Warn("SynchronizeSystemConfig() failure: ", err)
				}
				readErr = nil
			}

			// forward frames.
			for r.forwardArbiter.ShouldRun() {
				// encode frame.
				readBuf := buf[proto.ProtocolMessageHeaderSize:]
				err = lease.Tx(func(rw *water.Interface) error {
					read, readErr = rw.Read(readBuf)
					if readErr != nil {
						if readErr != io.EOF && readErr != os.ErrClosed && r.forwardArbiter.ShouldRun() {
							r.log.Error("read link failure: ", readErr)
						}
						return readErr
					}
					return nil
				})
				if readErr != nil && r.forwardArbiter.ShouldRun() {
					// bad lease. force to revoke.
					if err = lease.Revoke(); err != nil {
						r.log.Warn("force revoke failure: ", err)
					}
					break
				}
				if err != nil {
					break
				}
				if read < 1 {
					continue
				}

				// forward.
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
						go func(idx int) {
							r.forwardVTEPPeer(packed, peers[idx])
							wg.Done()
						}(idx)
					}
					wg.Wait()
					continue
				}
				if err = r.forwardVTEPPeer(packed, peers[0]); err != nil {
					r.log.Error("forwardVTEPPeer error:", err)
				}
			}
		}
	})
}
