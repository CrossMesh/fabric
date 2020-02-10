package route

import (
	"testing"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"github.com/stretchr/testify/assert"
)

func TestL2Router(t *testing.T) {
	arbiter := arbit.New(nil)
	frames := [][]byte{
		[]byte{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // src
			0x08, 0x06, // type: ARP
			// arp begin
			0x00, 0x01, 0x08, 0x00, 0x06, 0x04,
			0x00, 0x01, // opcode: request
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // sender MAC
			0x0a, 0x14, 0x01, 0x02, // sender IP
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // receiver MAC
			0x0a, 0x14, 0x01, 0x03, // recever IP.
		},
		[]byte{
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // dst
			0x38, 0xf9, 0xd3, 0x98, 0xef, 0xb3, // src
			0x08, 0x06, // type: ARP
			// arp begin
			0x00, 0x01, 0x08, 0x00, 0x06, 0x04,
			0x00, 0x02, // opcode: response
			0x38, 0xf9, 0xd3, 0x98, 0xef, 0xb3,
			0x0a, 0x14, 0x01, 0x03,
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab,
			0x0a, 0x14, 0x01, 0x02,
		},
		[]byte{
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // dst
			0x38, 0xf9, 0xd3, 0x98, 0xef, 0xb3, // src
			0x08, 0x00, // type: IPv4
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0x0a, 0x14, 0x01, 0x02, // src IP
			0x0a, 0x14, 0x01, 0x03, // dst IP
			// ...
		},
		[]byte{
			0x38, 0xf9, 0xd3, 0x98, 0xef, 0xb3, // dst
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // src
			0x08, 0x00, // type: IPv4
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0x0a, 0x14, 0x01, 0x03, // src IP: 10.20.1.3
			0x0a, 0x14, 0x01, 0x02, // dst IP: 10.20.1.2
			// ...
		},
		// test boardcast frame.
		[]byte{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // src
			0x08, 0x00, // type: IPv4
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0x0a, 0x14, 0x01, 0x03, // src IP: 10.20.1.3
			0x0a, 0x14, 0x01, 0x02, // dst IP: 10.20.1.2
			// ...
		},
		// test multicast STP frames.
		[]byte{
			0x01, 0x80, 0xC2, 0x00, 0x00, 0x00, // dst
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xab, // src
			0x08, 0x00, // type: IPv4
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			// ...
		},
	}

	peer := []*L2Peer{{PeerMeta{Self: true}}, {}, {}}
	assert.True(t, peer[0].Tx(func(p MembershipPeer, tx *PeerReleaseTx) bool {
		tx.Backend(&PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.1",
			},
			Disabled: false,
			Priority: 0,
		})
		return true
	}))
	assert.True(t, peer[1].Tx(func(p MembershipPeer, tx *PeerReleaseTx) bool {
		tx.Backend(&PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.2",
			},
			Disabled: false,
			Priority: 0,
		})
		return true
	}))
	assert.True(t, peer[2].Tx(func(p MembershipPeer, tx *PeerReleaseTx) bool {
		tx.Backend(&PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.3",
			},
			Disabled: false,
			Priority: 0,
		})
		return true
	}))

	g := NewGossipMembership()
	r := NewL2Router(arbiter, g, nil, time.Second*10)

	g.Discover(peer[0], peer[1])
	t.Run("backward", func(t *testing.T) {
		// ignore non-exist peer.
		p, learned := r.Backward(frames[1], peer[2].ActiveBackend().PeerBackendIdentity)
		assert.Nil(t, p)
		assert.False(t, learned, "non-exist peer should be ignored.")
		// learn first backend.
		p, learned = r.Backward(frames[1], peer[0].ActiveBackend().PeerBackendIdentity)
		assert.Equal(t, peer[0].Meta(), p.Meta())
		assert.True(t, learned)
		p, learned = r.Backward(frames[1], peer[0].ActiveBackend().PeerBackendIdentity)
		assert.Equal(t, peer[0].Meta(), p.Meta())
		assert.False(t, learned, "should not learn existing record.")

		// edge case: do not learn boardcast.
		faked := []byte{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // src
			0x08, 0x06, // type: ARP
		}
		p, learned = r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn boardcast address")

		// hot peer.
		ps := r.HotPeers(10 * time.Second)
		assert.NotNil(t, ps)
		assert.Equal(t, 1, len(ps))
		assert.Equal(t, peer[0].Meta(), ps[0].Meta())
	})
	g.Discover(peer[2])

	t.Run("forward", func(t *testing.T) {
		// boardcast when route not found.
		assert.Equal(t, 2, len(r.Forward(frames[2])))
		// normal unicast forward.
		ps := r.Forward(frames[3])
		assert.NotNil(t, ps)
		assert.Equal(t, 1, len(ps))
		assert.Equal(t, peer[0].Meta(), ps[0].Meta())
		// refresh learning.
		p, _ := r.Backward(frames[1], peer[1].ActiveBackend().PeerBackendIdentity)
		assert.Equal(t, peer[1].Meta(), p.Meta())
		ps = r.Forward(frames[3])
		assert.NotNil(t, ps)
		assert.Equal(t, 1, len(ps))
		assert.Equal(t, peer[1].Meta(), ps[0].Meta())

		// remove peer.
		assert.True(t, peer[1].Tx(func(p MembershipPeer, tx *PeerReleaseTx) bool {
			tx.State(gossip.DEAD, 2039)
			return true
		}))
		g.Clean(time.Now())
		ps = r.Forward(frames[3])
		assert.NotNil(t, ps)
		assert.Equal(t, 1, len(ps), "should boardcast, not ", ps)

		t.Run("hot_expired", func(t *testing.T) {
			p, learned := r.Backward(frames[1], peer[0].ActiveBackend().PeerBackendIdentity)
			assert.Equal(t, peer[0].Meta(), p.Meta())
			assert.True(t, learned)
			ps := r.Forward(frames[3])
			assert.NotNil(t, ps)
			assert.Equal(t, 1, len(ps))
			assert.Equal(t, peer[0].Meta(), ps[0].Meta())

			// wait for expired.
			r.expireHotMAC(time.Now().Add(time.Second * 30))
			ps = r.Forward(frames[3])
			assert.NotNil(t, ps)
			assert.Equal(t, 1, len(ps), "should boardcast, not ", ps)
		})
	})

	go arbiter.Shutdown()
	arbiter.Join()
}
