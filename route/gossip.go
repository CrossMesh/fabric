package route

import (
	"sort"
	"strconv"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/gossip"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	pbp "git.uestc.cn/sunmxt/utt/proto/pb"
)

type PeerReleaseTx struct {
	*gossip.PeerReleaseTx

	meta *PeerMeta

	backends []*PeerBackend
	version  uint64

	backendUpdated      bool
	updateActiveBackend bool
	versionUpdated      bool
}

func (t *PeerReleaseTx) UpdateActiveBackend() {
	t.updateActiveBackend = true
}

func (t *PeerReleaseTx) Backend(backends ...*PeerBackend) bool {
	if t.meta.backendEqual(backends...) {
		return false
	}
	t.backendUpdated = true
	t.backends = backends
	return true
}

func (t *PeerReleaseTx) Version(version uint64) {
	if version <= t.meta.version {
		t.versionUpdated, t.version = false, t.meta.version
		return
	}
	t.versionUpdated, t.version = true, version
}

func (t *PeerReleaseTx) IsNewVersion() bool {
	return t.versionUpdated
}

func (t *PeerReleaseTx) ShouldCommit() bool {
	return t.PeerReleaseTx.ShouldCommit() || t.backendUpdated || (t.versionUpdated && t.version > t.meta.version) || t.updateActiveBackend
}

type PeerBackend struct {
	backend.PeerBackendIdentity
	Disabled bool
	Priority uint32
}

type PeerMeta struct {
	gossip.Peer

	Self bool

	onBackendUpdated func(Peer, []backend.PeerBackendIdentity, []backend.PeerBackendIdentity)

	version           uint64
	backendByPriority []*PeerBackend
}

func (p *PeerMeta) Meta() *PeerMeta { return p }
func (p *PeerMeta) IsSelf() bool    { return p.Self }
func (p *PeerMeta) ActiveBackend() *PeerBackend {
	bes := p.backendByPriority
	if len(bes) < 1 {
		return &PeerBackend{
			PeerBackendIdentity: backend.PeerBackendIdentity{Type: pb.PeerBackend_UNKNOWN},
		}
	}
	return bes[0]
}

func (p *PeerMeta) updateActiveBackend() {
	cmp := func(i, j int) bool {
		ei, ej := p.backendByPriority[i], p.backendByPriority[j]
		if ei.Disabled != ej.Disabled {
			return !ei.Disabled // ei > ej ?
		}
		return ei.Priority > ej.Priority
	}
	if sort.SliceIsSorted(p.backendByPriority, cmp) {
		return
	}
	sort.Slice(p.backendByPriority, cmp)
}

func (p *PeerMeta) String() string {
	return p.Peer.String() + "," + strconv.FormatUint(p.version, 10)
}

func (p *PeerMeta) PBSnapshot() (msgPeer *pbp.Peer, err error) {
	state, stateVersion, _ := p.State()
	msgPeer = &pbp.Peer{
		Version:      p.version,
		StateVersion: stateVersion,
		State:        pb.Peer_State(state),
		Region:       p.Region(),
	}
	msgPeer.Backend = make([]*pbp.PeerBackend, 0, len(p.backendByPriority))

	for _, be := range p.backendByPriority {
		// underlay IP.
		msgBackend := &pbp.PeerBackend{
			Endpoint: be.Endpoint,
			Type:     be.Type,
			Priority: be.Priority,
		}
		msgPeer.Backend = append(msgPeer.Backend, msgBackend)
	}
	return
}

func (p *PeerMeta) applyPBSnapshot(tx *PeerReleaseTx, msg *pbp.Peer) {
	tx.Version(msg.Version)
	if !tx.IsNewVersion() {
		return
	}

	backends := make([]*PeerBackend, 0, len(msg.Backend))
	for idx := range msg.Backend {
		b := msg.Backend[idx]
		if b == nil {
			continue
		}
		backends = append(backends, &PeerBackend{
			Priority: b.Priority,
			Disabled: false,
			PeerBackendIdentity: backend.PeerBackendIdentity{
				Endpoint: b.Endpoint,
				Type:     b.Type,
			},
		})
	}
	tx.Backend(backends...)
	tx.Region(msg.Region)
	tx.State(int(msg.State), msg.StateVersion)
}

func (p *PeerMeta) ApplyPBSnapshot(msg *pbp.Peer) (err error) {
	if msg == nil {
		return nil
	}
	p.Tx(func(bp Peer, tx *PeerReleaseTx) bool {
		p.applyPBSnapshot(tx, msg)
		return tx.IsNewVersion()
	})
	return nil
}

func (p *PeerMeta) backendEqual(backends ...*PeerBackend) bool {
	if len(backends) != len(p.backendByPriority) {
		return false
	}
	set := map[backend.PeerBackendIdentity]struct{}{}
	for idx := range p.backendByPriority {
		set[p.backendByPriority[idx].PeerBackendIdentity] = struct{}{}
	}
	for idx := range backends {
		if _, exist := set[backend.PeerBackendIdentity{
			Type:     backends[idx].Type,
			Endpoint: backends[idx].Endpoint,
		}]; !exist {
			return false
		}
	}
	return true
}

func (p *PeerMeta) OnBackendUpdated(emit func(Peer, []backend.PeerBackendIdentity, []backend.PeerBackendIdentity)) {
	p.onBackendUpdated = emit
}

func (p *PeerMeta) RTx(commit func(Peer)) {
	p.Peer.RTx(func() {
		commit(p)
	})
}

func (p *PeerMeta) generateVersion() uint64 {
	// version should be monotonic increasing number over the cluster.
	// use unix nano as version since only publisher can create a new version.
	return uint64(time.Now().UnixNano()) // new version.
}

func (p *PeerMeta) Tx(commit func(Peer, *PeerReleaseTx) bool) (commited bool) {
	parentCommited := p.Peer.Tx(func(btx *gossip.PeerReleaseTx) bool {
		tx, shouldCommit := &PeerReleaseTx{
			meta:          p,
			PeerReleaseTx: btx,
		}, false

		shouldCommit = commit(p, tx)
		if !shouldCommit {
			return false
		}

		if !tx.backendUpdated && !tx.versionUpdated && !tx.updateActiveBackend {
			return true
		}
		commited = true

		// update version.
		if tx.versionUpdated {
			p.version = tx.version
		} else {
			p.version = p.generateVersion()
		}

		if tx.backendUpdated {
			// update backends.
			if onBackendUpdated := p.onBackendUpdated; onBackendUpdated != nil {
				olds, news := make([]backend.PeerBackendIdentity, 0, len(p.backendByPriority)), make([]backend.PeerBackendIdentity, 0, len(tx.backends))
				for _, be := range p.backendByPriority {
					olds = append(olds, be.PeerBackendIdentity)
				}
				for _, be := range tx.backends {
					news = append(news, be.PeerBackendIdentity)
				}
				onBackendUpdated(p, olds, news)
			}
			p.backendByPriority = make([]*PeerBackend, 0, len(tx.backends))
			for idx := range tx.backends {
				b := tx.backends[idx]
				if b == nil {
					continue
				}
				backend := b
				p.backendByPriority = append(p.backendByPriority, backend)
			}
			tx.updateActiveBackend = true
		}

		if tx.updateActiveBackend {
			p.updateActiveBackend()
		}

		return true
	})

	return parentCommited || commited
}

type GossipGroup struct {
	*gossip.Gossiper
}

func NewGossipGroup() *GossipGroup {
	return &GossipGroup{
		Gossiper: gossip.NewGossiper(),
	}
}

func (s *GossipGroup) PBSnapshot() (peers []*pbp.Peer, err error) {
	peers = make([]*pbp.Peer, 0, 8)
	s.VisitPeer(func(region string, p gossip.MembershipPeer) bool {
		peer := p.(Peer)
		pbMsg, merr := peer.PBSnapshot()
		if merr != nil {
			err = merr
			return false
		}
		peers = append(peers, pbMsg)
		return true
	})
	return
}
