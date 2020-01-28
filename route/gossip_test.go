package route

import (
	"testing"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"github.com/stretchr/testify/assert"
)

type TestPeer struct {
	PeerMeta
}

func TestPeerMetaProtobuf(t *testing.T) {
	p := TestPeer{}
	b := &PeerBackend{
		PeerBackendIdentity: backend.PeerBackendIdentity{
			Type:     pb.PeerBackend_TCP,
			Endpoint: "172.17.0.1",
		},
		Disabled: false,
		Priority: 0,
	}
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Backend(b)
		return true
	}))
	msg, err := p.PBSnapshot()
	assert.NoError(t, err)
	assert.Equal(t, p.version, msg.GetVersion())
	assert.Equal(t, 1, len(msg.Backend))
	assert.Equal(t, b.Priority, msg.Backend[0].Priority)
	assert.Equal(t, b.Endpoint, msg.Backend[0].Endpoint)
	assert.Equal(t, b.Type, msg.Backend[0].Type)
}

func TestPeerMeta(t *testing.T) {
	p := TestPeer{}
	p.Self = true
	assert.True(t, p.IsSelf())
	p.RTx(func(Peer) {})

	backends := []*PeerBackend{
		{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.1",
			},
			Disabled: false,
			Priority: 0,
		},
		{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.2",
			},
			Disabled: false,
			Priority: 1,
		},
		{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.3",
			},
			Disabled: true,
			Priority: 2,
		},
	}
	t.Log(p.String())
	// empty
	assert.False(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		return true
	}))
	// ActiveBackend() should give PeerBackend_UNKNOWN when no backend activated.
	assert.Equal(t, pb.PeerBackend_UNKNOWN, p.ActiveBackend().Type)
	// transaction canceled.
	assert.False(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Backend(backends...)
		assert.True(t, tx.ShouldCommit())
		return false
	}))
	// commit on backend changed
	p.OnBackendUpdated(func(p Peer, old []backend.PeerBackendIdentity, new []backend.PeerBackendIdentity) {
		assert.Equal(t, 0, len(old))
		assert.Equal(t, 3, len(new))
	})
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Backend(backends...)
		assert.True(t, tx.ShouldCommit())
		return true
	}))
	t.Log(p.String())
	assert.Equal(t, backends[1].Endpoint, p.ActiveBackend().Endpoint)
	assert.Equal(t, backends[1].Type, p.ActiveBackend().Type)
	p.OnBackendUpdated(nil)
	// do not commit when no backend changed
	assert.False(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Backend(backends...)
		assert.False(t, tx.ShouldCommit())
		return true
	}))
	// should commit when any state of parent changed.
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Region("dc1")
		return true
	}))
	t.Log(p.String())
	// do not accept old version.
	assert.False(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Version(312389)
		assert.False(t, tx.ShouldCommit())
		return true
	}))
	// update version.
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Version(uint64(time.Now().UnixNano()))
		assert.True(t, tx.ShouldCommit())
		return true
	}))
	t.Log(p.String())
	// update active backend.
	backends[2].Disabled = false
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.UpdateActiveBackend()
		assert.True(t, tx.ShouldCommit())
		return true
	}))
	assert.Equal(t, backends[2].Endpoint, p.ActiveBackend().Endpoint)
	assert.Equal(t, backends[2].Type, p.ActiveBackend().Type)
	t.Log(p.String())

	// add new backend.
	backends = append(backends,
		&PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Type:     pb.PeerBackend_TCP,
				Endpoint: "172.17.0.3",
			},
			Disabled: true,
			Priority: 2,
		},
	)
	p.OnBackendUpdated(func(p Peer, old []backend.PeerBackendIdentity, new []backend.PeerBackendIdentity) {
		assert.Equal(t, 3, len(old))
		assert.Equal(t, 4, len(new))
	})
	assert.True(t, p.Tx(func(p Peer, tx *PeerReleaseTx) bool {
		tx.Backend(backends...)
		assert.True(t, tx.ShouldCommit())
		return true
	}))
	t.Log(p.String())
}
