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

	fromRef, _ := peerSet[from.HashID()]
	if fromRef == nil {
		// drop frame from an unknown peer.
		return
	}

	copy(dst[:], frame[0:6])
	copy(src[:], frame[6:12])
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
			if peer := ref.peer; from.IsSelf() != peer.IsSelf() {
				peers = append(peers, peer)
			}
		}
	}

	// learn.
	if 0 == bytes.Compare(src[:], EthernetBoardcastAddress[:]) {
		// do not learn boardcast address.
		return
	}

	origin, hasRoute := routes[src]
	if hasRoute && origin == from {
		return
	}

	// try to update routes.
	r.lock.Lock()

	routes, peerSet = r.mac2Peer, r.peers
	if origin, hasRoute = routes[src]; hasRoute && origin == from { // learned.
		r.lock.Unlock()
		return
	}
	if origin != nil {
		if ref, _ := peerSet[origin.HashID()]; ref != nil { // should has peer.
			ref.lock.Lock()
			delete(ref.macSet, src)
			ref.lock.Unlock()
		}
	}
	fromRef.lock.Lock()
	fromRef.macSet[src] = struct{}{}
	fromRef.lock.Unlock()

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
	if ref, hasPeer := peers[id]; hasPeer && peer == ref.peer {
		return
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	peers = r.peers
	if ref, hasPeer := peers[id]; hasPeer && peer == ref.peer {
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
	for mac, target := range routes {
		if _, exist := ref.macSet[mac]; exist && peer == target {
			continue
		}
		newRoutes[mac] = target
	}
	r.mac2Peer = newRoutes // replace the old.
	ref.lock.Unlock()

	// peer updates.
	newPeers := make(map[string]*p2pL2MeshPeerRef, len(peers))
	for pid, peer := range peers {
		if pid == id {
			continue
		}
		newPeers[pid] = peer
	}
	r.peers = newPeers
}
