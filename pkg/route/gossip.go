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
}
type PeerBackend struct {
	Type     pbp.PeerBackend_BackendType
	Endpoint string
}

type PeerMeta struct {
	gossip.Peer

	version           uint64
	backendByPriority []*peerBackend
	activeBackend     int
}

func (p *PeerMeta) Init() {
	p.version = 0
	p.activeBackend = 0
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

func (p *PeerMeta) Tx(commit func(*PeerReleaseTx) bool) bool {
	return p.Peer.Tx(func(btx *gossip.PeerReleaseTx) bool {
		tx, shouldCommit := &PeerReleaseTx{
			meta:          p,
			PeerReleaseTx: btx,
		}, false

		shouldCommit = commit(tx)
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

		// update backends.
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
		sort.Slice(p.backendByPriority, func(i, j int) bool {
			return p.backendByPriority[i].Priority > p.backendByPriority[j].Priority
		})

		return true
	})
}
