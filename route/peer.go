package route

// MeshNetPeer is abstraction of mesh network peer.
type MeshNetPeer interface {
	ID() string
	IsSelf() bool
}
