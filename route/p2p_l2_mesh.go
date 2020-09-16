package route

import (
	"bytes"
	"sync"
)

var (
	EthernetBoardcastAddress = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

type p2pL2MeshPeerRef struct {
	lock sync.RWMutex

	peer   MeshNetPeer
	macSet map[[6]byte]struct{}
}

// P2PL2MeshNetworkRouter implements symmetry peer-to-peer ethernet network.
type P2PL2MeshNetworkRouter struct {
	lock     sync.RWMutex
	mac2Peer map[[6]byte]MeshNetPeer      // (copy-on-write)
	peers    map[string]*p2pL2MeshPeerRef // (copy-on-write)
}

// NewP2PL2MeshNetworkRouter initializes new P2PL2MeshNetworkRuter.
func NewP2PL2MeshNetworkRouter() *P2PL2MeshNetworkRouter {
	return &P2PL2MeshNetworkRouter{
		peers:    make(map[string]*p2pL2MeshPeerRef),
		mac2Peer: make(map[[6]byte]MeshNetPeer),
	}
}

// Route routes packet.
// param `frame` is ethernet frame.
func (r *P2PL2MeshNetworkRouter) Route(frame []byte, from MeshNetPeer) (peers []MeshNetPeer) {
	// This function will be massively called. Be careful for performance penalty.

	var dst, src [6]byte

	if len(frame) < 14 {
		// frame too small
		return nil
	}

	routes, peerSet := r.mac2Peer, r.peers // for lock-free read, must copy a reference first.

	copy(dst[:], frame[0:6])
	if 0 != bytes.Compare(dst[:], EthernetBoardcastAddress[:]) { // not boardcast.
		if dst[0]&0x01 != 0 { // multicast not supported now.
			return nil
		}
		peer, hasPeer := routes[dst]
		if hasPeer && peer != nil {
			peers = []MeshNetPeer{peer.(MeshNetPeer)}
		}
	}
	if len(peers) < 1 { // boardcast.
		for _, ref := range peerSet {
			peer := ref.peer
			if peer.IsSelf() {
				continue
			}
			peers = append(peers, peer)
		}
	}

	// learn.
	copy(src[:], frame[6:12])
	if 0 == bytes.Compare(src[:], EthernetBoardcastAddress[:]) {
		// do not learn boardcast address.
		return
	}

	origin, hasRoute := routes[src]
	if hasRoute && origin == from {
		return
	}

	peerRef, _ := peerSet[from.HashID()]
	if peerRef == nil {
		// do not learn route for an unknown peer.
		return
	}

	// try to update routes.
	r.lock.Lock()

	routes, peerSet = r.mac2Peer, r.peers
	if origin, hasRoute = routes[src]; hasRoute && origin == from { // learned.
		r.lock.Unlock()
		return
	}
	if ref, _ := peerSet[origin.HashID()]; ref != nil { // should has peer.
		ref.lock.Lock()
		delete(ref.macSet, src)
		ref.lock.Unlock()
	}
	peerRef.lock.Lock()
	peerRef.macSet[src] = struct{}{}
	peerRef.lock.Unlock()

	// route updates.
	newRoutes := make(map[[6]byte]MeshNetPeer, len(routes))
	for mac, peer := range routes {
		newRoutes[mac] = peer
	}
	newRoutes[src] = from
	r.mac2Peer = newRoutes // replace the old.

	r.lock.Unlock()

	return
}

// PeerJoin joins new peer.
func (r *P2PL2MeshNetworkRouter) PeerJoin(peer MeshNetPeer) {
	if peer == nil {
		return
	}
	id := peer.HashID()
	if id == "" {
		return
	}

	peers := r.peers
	if ref := peers[id]; peer == ref.peer {
		return
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	peers = r.peers
	if ref := peers[id]; peer == ref.peer {
		return
	}
	newPeers := make(map[string]*p2pL2MeshPeerRef, len(peers))
	for id, peer := range peers {
		newPeers[id] = peer
	}
	newPeers[id] = &p2pL2MeshPeerRef{
		peer:   peer,
		macSet: make(map[[6]byte]struct{}),
	}
	r.peers = newPeers
}

// PeerLeave removes peer and related routes.
func (r *P2PL2MeshNetworkRouter) PeerLeave(peer MeshNetPeer) {
	if peer == nil {
		return
	}
	id := peer.HashID()
	if id == "" {
		return
	}

	peers := r.peers
	if ref := peers[id]; peer != ref.peer {
		return
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	routes, peers := r.mac2Peer, r.peers
	ref := peers[id]
	if peer != ref.peer {
		return
	}

	ref.lock.Lock()
	// route updates.
	newRoutes := make(map[[6]byte]MeshNetPeer, len(routes))
	for mac, peer := range routes {
		if _, exist := ref.macSet[mac]; exist {
			continue
		}
		newRoutes[mac] = peer
	}
	r.mac2Peer = newRoutes // replace the old.
	ref.lock.Unlock()

	// peer updates.
	newPeers := make(map[string]*p2pL2MeshPeerRef, len(peers))
	for oid, peer := range peers {
		if oid == id {
			continue
		}
		newPeers[id] = peer
	}
	r.peers = newPeers
}
