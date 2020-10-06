package route

type MockMeshNetPeer struct {
	Self bool
	ID   string
}

func (p *MockMeshNetPeer) HashID() string { return p.ID }
func (p *MockMeshNetPeer) IsSelf() bool   { return p.Self }
