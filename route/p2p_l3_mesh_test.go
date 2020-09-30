package route

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestP2PL3Mesh(t *testing.T) {
	packet := [][]byte{
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			10, 240, 4, 2, // src IP: 10.240.4.2
			10, 240, 5, 1, // dst IP: 10.240.5.1
		},
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			10, 240, 5, 1, // src IP: 10.240.5.1
			10, 240, 4, 2, // dst IP: 10.240.4.2
		},
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			10, 240, 5, 1, // src IP: 10.240.5.1
			10, 240, 5, 3, // dst IP: 10.240.5.3
		},

		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			10, 240, 5, 2, // src IP: 10.240.5.2
			224, 0, 1, 75, // dst IP: 224.0.1.75
		},
		[]byte{
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			0xd5, 0xed,
			10, 240, 5, 1, // src IP: 10.240.5.2
			224, 0, 1, 75, // dst IP: 224.0.1.75
		},
	}

	t.Run("normal", func(t *testing.T) {
		route := NewP2PL3IPv4MeshNetworkRouter()
		self := &MockMeshNetPeer{Self: true, ID: "self"}
		peer1 := &MockMeshNetPeer{Self: false, ID: "peer1"}
		peer2 := &MockMeshNetPeer{Self: false, ID: "peer2"}
		peer3 := &MockMeshNetPeer{Self: false, ID: "peer3"}
		peer4 := &MockMeshNetPeer{Self: false, ID: "peer4"}

		route.PeerJoin(self)
		route.PeerJoin(peer1)
		route.PeerJoin(peer2)
		route.PeerJoin(peer3)

		// accept packet in case of unicast miss.
		peers := route.Route(packet[1], peer2)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(self))
		route.PeerLeave(peer2)
		route.PeerJoin(peer2)

		// unicast miss.
		peers = route.Route(packet[0], self)
		assert.Equal(t, 3, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer1))
		assert.Contains(t, peers, MeshNetPeer(peer2))
		assert.Contains(t, peers, MeshNetPeer(peer3))
		// unicast from the remote.
		peers = route.Route(packet[1], peer2)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(self))
		// updated unicast route.
		peers = route.Route(packet[1], peer1)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(self))
		peers = route.Route(packet[0], self)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer1))

		// drop packet from unknown peer.
		peers = route.Route(packet[2], peer4)
		assert.Equal(t, 0, len(peers))

		// drop incoming multicast packet.
		peers = route.Route(packet[3], peer2)
		assert.Equal(t, 0, len(peers))

		// do not send multicast frame.
		peers = route.Route(packet[4], self)
		assert.Equal(t, 0, len(peers))

		// remove routes when peer leaves.
		route.PeerLeave(peer2)
		peers = route.Route(packet[1], self)
		assert.Equal(t, 2, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer1))
		assert.Contains(t, peers, MeshNetPeer(peer3))
	})

	t.Run("static_routes", func(t *testing.T) {
		epkt := [][]byte{
			[]byte{
				0x45, 0x00,
				0x00, 0x54, // length.
				0xa8, 0x52, 0x00, 0x00, 0x40,
				0x01, // type: icmp
				0xd5, 0xed,
				10, 240, 5, 2, // src IP: 10.240.5.1
				10, 240, 4, 2, // dst IP: 10.240.4.2
			},
			[]byte{
				0x45, 0x00,
				0x00, 0x54, // length.
				0xa8, 0x52, 0x00, 0x00, 0x40,
				0x01, // type: icmp
				0xd5, 0xed,
				10, 240, 4, 2, // src IP: 10.240.4.2
				10, 240, 3, 2, // dst IP: 10.240.3.2
			},
		}

		route := NewP2PL3IPv4MeshNetworkRouter()
		self := &MockMeshNetPeer{Self: true, ID: "self"}
		peer1 := &MockMeshNetPeer{Self: false, ID: "peer1"}
		peer2 := &MockMeshNetPeer{Self: false, ID: "peer2"}
		peer3 := &MockMeshNetPeer{Self: false, ID: "peer3"}
		peer4 := &MockMeshNetPeer{Self: false, ID: "peer4"}

		route.PeerJoin(self)
		route.PeerJoin(peer1)
		route.PeerJoin(peer2)
		route.PeerJoin(peer3)
		route.PeerJoin(peer4)

		var subnet [3]*net.IPNet
		var err error

		_, subnet[0], err = net.ParseCIDR("10.240.5.0/24")
		assert.NoError(t, err)
		_, subnet[1], err = net.ParseCIDR("10.240.5.0/25")
		assert.NoError(t, err)
		_, subnet[2], err = net.ParseCIDR("10.240.4.0/24")
		assert.NoError(t, err)
		assert.Error(t, route.AddStaticCIDRRoutes(peer1, subnet[0], subnet[1]))
		assert.NoError(t, route.AddStaticCIDRRoutes(peer1, subnet[0]))
		assert.Error(t, route.AddStaticCIDRRoutes(self, subnet[1]))
		assert.NoError(t, route.AddStaticCIDRRoutes(peer1, subnet[2]))
		_, subnet[0], err = net.ParseCIDR("10.240.3.0/24")
		assert.NoError(t, err)
		_, subnet[1], err = net.ParseCIDR("10.240.2.0/24")
		assert.NoError(t, err)
		_, subnet[2], err = net.ParseCIDR("10.240.1.0/24")
		assert.NoError(t, err)
		assert.NoError(t, route.AddStaticCIDRRoutes(peer2, subnet[0]))
		assert.NoError(t, route.AddStaticCIDRRoutes(peer3, subnet[1]))
		assert.NoError(t, route.AddStaticCIDRRoutes(peer4, subnet[2]))

		// unicast should not miss.
		peers := route.Route(packet[0], self)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer1))
		peers = route.Route(epkt[0], peer1)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(self))
		peers = route.Route(epkt[1], self)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer2))

		// remote static route.
		assert.False(t, route.RemoveStaticCIDRRoutes(peer3, subnet[0]))
		assert.False(t, route.RemoveStaticCIDRRoutes(peer2, subnet[2]))
		peers = route.Route(epkt[1], self)
		assert.Equal(t, 1, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer2))
		assert.True(t, route.RemoveStaticCIDRRoutes(peer2, subnet[0]))
		peers = route.Route(epkt[1], self)
		assert.Equal(t, 4, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer1))
		assert.Contains(t, peers, MeshNetPeer(peer2))
		assert.Contains(t, peers, MeshNetPeer(peer3))
		assert.Contains(t, peers, MeshNetPeer(peer4))

		// remove routes when peer leaves.
		route.PeerLeave(peer1)
		peers = route.Route(packet[1], self)
		assert.Equal(t, 3, len(peers))
		assert.Contains(t, peers, MeshNetPeer(peer2))
		assert.Contains(t, peers, MeshNetPeer(peer3))
		assert.Contains(t, peers, MeshNetPeer(peer4))
	})
}
