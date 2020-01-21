package route

import (
	"sort"
	"strconv"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/backend"
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	pbp "git.uestc.cn/sunmxt/utt/pkg/proto/pb"
)

type PeerReleaseTx struct {
	*gossip.PeerReleaseTx

	meta *PeerMeta

	backends []backend.Backend
	version  uint64

	backendUpdated bool
	versionUpdated bool
}

func (t *PeerReleaseTx) Backend(backends ...backend.Backend) bool {
	if t.meta.backendEqual(backends...) {
		return false
	}
	t.backendUpdated = true
	t.backends = backends
	return true
}

func (t *PeerReleaseTx) Version(version uint64) {
	t.versionUpdated = true
	t.version = version
}

func (t *PeerReleaseTx) ShouldCommit() bool {
	return t.PeerReleaseTx.ShouldCommit() || t.backendUpdated || (t.versionUpdated && t.version > t.meta.version)
}

type peerBackend struct {
	PeerBackend
	Priority uint32
	Disabled bool
}

type PeerBackend struct {
	Type     pbp.PeerBackend_BackendType
	Endpoint string
}

type PeerMeta struct {
	gossip.Peer

	Self bool

	onBackendUpdated func(Peer, []PeerBackend, []PeerBackend)

	version           uint64
	backendByPriority []*peerBackend
}

func (p *PeerMeta) Meta() *PeerMeta { return p }
func (p *PeerMeta) IsSelf() bool    { return p.Self }
func (p *PeerMeta) ActiveBackend() PeerBackend {
	bes := p.backendByPriority
	if len(bes) < 1 {
		return PeerBackend{Type: pbp.PeerBackend_UNKNOWN}
	}
	return bes[0].PeerBackend
}

func (p *PeerMeta) updateActiveBackend() {
	cmp := func(i, j int) bool {
		ei, ej := p.backendByPriority[i], p.backendByPriority[j]
		if ei.Disabled != ej.Disabled {
			if ei.Disabled {
				return true // ei > ej
			}
			return false
		}
		return ei.Priority > ej.Priority
	}
	if sort.SliceIsSorted(p.backendByPriority, cmp) {
		return
	}
	sort.Slice(p.backendByPriority, cmp)
}

func (p *PeerMeta) String() string {
	state, stateVersion, _ := p.State()
	stateName, _ := gossip.StateName[state]
	if stateName == "" {
		stateName = "unknown"
	}
	return "(" + stateName + "," + strconv.FormatInt(int64(stateVersion), 10) + ")," +
		strconv.FormatUint(p.version, 10)
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
			Backend:  be.Type,
			Priority: be.Priority,
		}
		msgPeer.Backend = append(msgPeer.Backend, msgBackend)
	}
	return
}

func (p *PeerMeta) backendEqual(backends ...backend.Backend) bool {
	if len(backends) != len(p.backendByPriority) {
		return false
	}
	set := map[PeerBackend]struct{}{}
	for idx := range p.backendByPriority {
		set[p.backendByPriority[idx].PeerBackend] = struct{}{}
	}
	for idx := range backends {
		if _, exist := set[PeerBackend{
			Type:     backends[idx].Type(),
			Endpoint: backends[idx].Publish(),
		}]; !exist {
			return false
		}
	}
	return true
}

func (p *PeerMeta) OnBackendUpdated(emit func(Peer, []PeerBackend, []PeerBackend)) {
	p.onBackendUpdated = emit
}

func (p *PeerMeta) RTx(commit func(Peer)) {
	p.Peer.RTx(func() {
		commit(p)
	})
}

func (p *PeerMeta) Tx(commit func(Peer, *PeerReleaseTx) bool) bool {
	return p.Peer.Tx(func(btx *gossip.PeerReleaseTx) bool {
		tx, shouldCommit := &PeerReleaseTx{
			meta:          p,
			PeerReleaseTx: btx,
		}, false

		shouldCommit = commit(p, tx)
		if !shouldCommit {
			return false
		}

		if !tx.backendUpdated && (!tx.versionUpdated || tx.version <= tx.meta.version) {
			return true
		}

		// update version.
		if tx.versionUpdated {
			p.version = tx.version
		} else {
			// version should be monotonic increasing number over the cluster.
			// use unix nano as version since only publisher can create a new version.
			p.version = uint64(time.Now().UnixNano()) // new version.
		}

		if tx.backendUpdated {
			// update backends.
			if onBackendUpdated := p.onBackendUpdated; onBackendUpdated != nil {
				olds, news := make([]PeerBackend, len(p.backendByPriority)), make([]PeerBackend, len(tx.backends))

				onBackendUpdated(p, olds, news)
			}
			p.backendByPriority = make([]*peerBackend, 0, len(tx.backends))
			for idx := range tx.backends {
				b := tx.backends[idx]
				if b == nil {
					continue
				}
				backend := &peerBackend{
					Priority: b.Priority(),
					PeerBackend: PeerBackend{
						Endpoint: b.Publish(),
						Type:     b.Type(),
					},
				}
				p.backendByPriority = append(p.backendByPriority, backend)
			}
			p.updateActiveBackend()
		}

		return true
	})
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
