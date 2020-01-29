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

func TestL3Router(t *testing.T) {
	arbiter := arbit.New(nil)
	packet := [][]byte{
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0x0a, 0xF0, 0x04, 0x02, // src IP: 10.240.4.2
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		},
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0x0a, 0xF0, 0x05, 0x01, // src IP: 10.240.5.1
			0x0a, 0xF0, 0x04, 0x02, // dst IP: 10.240.4.2
		},
	}

	peer := []*L3Peer{{}, {}, {}}
	assert.True(t, peer[1].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
		assert.NoError(t, tx.CIDR("10.240.5.1/24"))
		return true
	}))
	assert.True(t, peer[0].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
		assert.NoError(t, tx.CIDR("10.240.4.1/24"))
		tx.IsRouter(true)
		return true
	}))
	assert.True(t, peer[2].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
		assert.NoError(t, tx.CIDR("10.240.4.3/24"))
		tx.IsRouter(true)
		return true
	}))

	assert.True(t, peer[0].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
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
	assert.True(t, peer[1].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
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
	assert.True(t, peer[2].Tx(func(p Peer, tx *L3PeerReleaseTx) bool {
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

	r := NewL3Router(arbiter, nil, time.Second*10)
	r.Gossip().Seed(peer[0])
	r.Gossip().Seed(peer[1])
	r.Gossip().Do(0, func(*gossip.GossipContext) {})

	t.Run("invalid", func(t *testing.T) {
		// case: do not learn boardcast.
		faked := []byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0xff, 0xff, 0xff, 0xff, // src IP: 239.1.1.1
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		}
		_, learned := r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn boardcast address")
		// case: do not learn multicast.
		faked = []byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			0xef, 0x01, 0x01, 0x01, // src IP: 239.1.1.1
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		}
		_, learned = r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn multicast address")
		// case: do not learn loopback.
		faked = []byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			127, 1, 1, 1, // src IP: 127.1.1.1
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		}
		_, learned = r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn loopback address")
		// case: do not learn link local.
		faked = []byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			169, 254, 1, 1, // src IP: 127.1.1.1
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		}
		_, learned = r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn link local unicast.")
		faked = []byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			224, 0, 0, 3, // src IP
			0x0a, 0xF0, 0x05, 0x01, // dst IP: 10.240.5.1
		}
		_, learned = r.Backward(faked, peer[1].ActiveBackend().PeerBackendIdentity)
		assert.False(t, learned, "should not learn link local multicast.")
	})

	t.Run("backward", func(t *testing.T) {
		// ignore non-exist peer.
		p, learned := r.Backward(packet[1], peer[2].ActiveBackend().PeerBackendIdentity)
		assert.Nil(t, p)
		assert.False(t, learned, "non-exist peer should be ignored.")
		// learn first backend.
		p, learned = r.Backward(packet[1], peer[1].ActiveBackend().PeerBackendIdentity)
		assert.Equal(t, peer[0].GossiperStub(), p.GossiperStub())
		assert.True(t, learned)
		p, learned = r.Backward(packet[1], peer[1].ActiveBackend().PeerBackendIdentity)
		assert.Equal(t, peer[0].GossiperStub(), p.GossiperStub())
		assert.False(t, learned, "should not learn existing record.")

		// hot peer.
		ps := r.HotPeers(10 * time.Second)
		assert.NotNil(t, ps)
		assert.Equal(t, 1, len(ps))
		assert.Equal(t, peer[0].GossiperStub(), ps[0].GossiperStub())
	})
	r.Gossip().Seed(peer[2])
	r.Gossip().Do(0, func(*gossip.GossipContext) {})

	t.Run("forward", func(t *testing.T) {
		// select peers by subnet when route not found.
		target := r.Forward(packet[1])
		assert.Equal(t, 1, len(target))
		p := target[0].(*L3Peer)
		assert.NotEqual(t, "10.240.4.2", p.ip.String())
		assert.True(t, p.ip.String() == peer[0].ip.String() || p.ip.String() == peer[2].ip.String())

		// normal route.
		target = r.Forward(packet[0])
		assert.Equal(t, 1, len(target))
		t.Log(target[0])
		p = target[0].(*L3Peer)
		assert.Equal(t, peer[1].ip.String(), p.ip.String())
	})

	go arbiter.Shutdown()
	arbiter.Join()
}
