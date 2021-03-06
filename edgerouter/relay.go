package edgerouter

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/fabric/proto"
	"github.com/songgao/water"
)

type forwardStatistics struct {
	read uint32
	sent uint32
}

var (
	ErrRelayNoBackend = errors.New("backend unavaliable")
)

func (r *EdgeRouter) writeLocalVTEP(lease *vtepQueueLease, frame []byte) (err error) {
	if err = lease.Tx(func(rw *water.Interface) error {
		_, err := rw.Write(frame)
		return err
	}); err != nil && err != ErrVTEPQueueRevoke {
		r.log.Errorf("fail to write VTEP. (err = \"%v\")", err)
		return err
	}
	return nil
}

func (r *EdgeRouter) receiveRemote(msg *metanet.Message) {
	peers := r.route.Route(msg.Payload, msg.Peer())

	eli, isSelf := 0, false
	for i := 0; i < len(peers); i++ {
		peer := peers[i]
		if peer == nil {
			continue
		}
		if peer.IsSelf() {
			isSelf = true
			continue
		}
		if i != eli {
			peers[eli] = peers[i]
		}
	}
	peers = peers[:eli]
	for isSelf {
		lease, err := r.vtep.QueueLease()
		if err != nil {
			r.log.Errorf("cannot acquire queue lease. (err = \"%v\")", err)
			break
		}
		if lease == nil {
			break
		}
		r.writeLocalVTEP(lease, msg.Payload)
		break
	}
	for _, p := range peers {
		// TODO(xutao): optimization.
		r.metaNet.SendToPeers(proto.MsgTypeRawFrame, msg.Payload, p.(*metanet.MetaPeer))
	}
}

func (r *EdgeRouter) goForwardVTEP() {
	buf := make([]byte, 2048)

	r.arbiters.forward.Go(func() {
		var (
			lease        *vtepQueueLease
			err, readErr error
			read         int
			peers        []*metanet.MetaPeer
		)

		for r.arbiters.forward.ShouldRun() {
			// acquire queue lease.
			for r.arbiters.forward.ShouldRun() {
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
			for r.arbiters.forward.ShouldRun() {
				// encode frame.
				readBuf := buf[:]
				err = lease.Tx(func(rw *water.Interface) error {
					read, readErr = rw.Read(readBuf)
					if readErr != nil {
						if readErr != io.EOF && readErr != os.ErrClosed && r.arbiters.forward.ShouldRun() {
							r.log.Error("read link failure: ", readErr)
						}
						return readErr
					}
					return nil
				})
				if readErr != nil && r.arbiters.forward.ShouldRun() {
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
				readBuf = readBuf[:read]

				// forward.
				isSelf := false
				meshPeers := r.route.Route(readBuf, r.metaNet.Publish.Self)
				if len(meshPeers) < 1 {
					continue
				}
				peers = peers[:0]
				for _, metaPeer := range meshPeers {
					if metaPeer == nil {
						continue
					}
					if metaPeer.IsSelf() {
						isSelf = true
						continue
					}
					peer, isPeer := metaPeer.(*metanet.MetaPeer)
					if !isPeer {
						continue
					}
					peers = append(peers, peer)
				}
				if isSelf {
					r.writeLocalVTEP(lease, readBuf)
				}
				if len(peers) > 0 {
					r.metaNet.SendToPeers(proto.MsgTypeRawFrame, readBuf, peers...)
				}
			}
		}
	})
}
