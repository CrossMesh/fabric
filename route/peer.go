package route

// MeshNetPeer is abstraction of mesh network peer.
type MeshNetPeer interface {
	HashID() string
	IsSelf() bool
}
