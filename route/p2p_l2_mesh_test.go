package route

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestP2PL2Mesh(t *testing.T) {
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
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xb3, // dst
			0x38, 0xf9, 0xee, 0x98, 0xef, 0xb3, // src
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
			0x38, 0xf9, 0xee, 0x98, 0xef, 0xb3, // dst
			0xf6, 0xd4, 0xbd, 0x58, 0x72, 0xb3, // src
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
			0xf6, 0xd4, 0xbd, 0x58, 0x78, 0xab, // src
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
			0xf6, 0xd4, 0xbd, 0x58, 0x78, 0xab, // src
			0x08, 0x00, // type: IPv4
			0x45, 0x00,
			0x00, 0x54, // length.
			0xa8, 0x52, 0x00, 0x00, 0x40,
			0x01, // type: icmp
			// ...
		},
	}

	route := NewP2PL2MeshNetworkRouter()
	self := &MockMeshNetPeer{Self: true, ID: "self"}
	peer1 := &MockMeshNetPeer{Self: false, ID: "peer1"}
	peer2 := &MockMeshNetPeer{Self: false, ID: "peer2"}
	peer3 := &MockMeshNetPeer{Self: false, ID: "peer3"}
	peer4 := &MockMeshNetPeer{Self: false, ID: "peer4"}
	route.PeerJoin(self)
	route.PeerJoin(peer1)
	route.PeerJoin(peer2)
	route.PeerJoin(peer3)

	// arp. receive req.
	peers := route.Route(frames[0], peer3)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(self))
	route.PeerLeave(peer3)
	route.PeerJoin(peer3)

	// arp. req.
	peers = route.Route(frames[0], self)
	assert.Equal(t, 3, len(peers))
	assert.Contains(t, peers, MeshNetPeer(peer1))
	assert.Contains(t, peers, MeshNetPeer(peer2))
	assert.Contains(t, peers, MeshNetPeer(peer3))
	// arp. resp.
	peers = route.Route(frames[1], peer1)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(self))

	// unicast miss.
	peers = route.Route(frames[2], self)
	assert.Equal(t, 3, len(peers))
	assert.Contains(t, peers, MeshNetPeer(peer1))
	assert.Contains(t, peers, MeshNetPeer(peer2))
	assert.Contains(t, peers, MeshNetPeer(peer3))

	// drop frame from unknown peer.
	peers = route.Route(frames[3], peer4)
	assert.Equal(t, 0, len(peers))
	// unicast from the remote.
	peers = route.Route(frames[3], peer3)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(self))
	peers = route.Route(frames[2], self)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(peer3))
	// updated unicast route.
	peers = route.Route(frames[3], peer2)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(self))
	peers = route.Route(frames[2], self)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(peer2))

	// do not forward boardcast frame. buf accept it myself.
	peers = route.Route(frames[4], peer3)
	assert.Equal(t, 1, len(peers))
	assert.Contains(t, peers, MeshNetPeer(self))

	// drop incoming multicast frame.
	peers = route.Route(frames[5], peer3)
	assert.Equal(t, 0, len(peers))

	// do not send multicast frame.
	peers = route.Route(frames[5], self)
	assert.Equal(t, 0, len(peers))

	// remove routes when peer leaves.
	route.PeerLeave(peer2)
	peers = route.Route(frames[2], self) // since the route is removed, this should miss and boardcast.
	assert.Equal(t, 2, len(peers))
	assert.Contains(t, peers, MeshNetPeer(peer1))
	assert.Contains(t, peers, MeshNetPeer(peer3))

}
