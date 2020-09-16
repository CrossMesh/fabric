package route

import (
	"errors"
)

var (
	ErrNoMoreShard = errors.New("cannot allocate more shard")
)

// PeerActivityWatcher watches peer membership changes.
type PeerActivityWatcher interface {
	PeerJoin(peer MeshNetPeer)
	PeerLeave(peer MeshNetPeer)
}

// MeshDataNetworkRouter routes packet over mesh network.
type MeshDataNetworkRouter interface {
	Route(raw []byte, from MeshNetPeer) []MeshNetPeer
}
