package route

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/backend"
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	pbp "git.uestc.cn/sunmxt/utt/pkg/proto/pb"
)

type peerMetaTx struct {
	backends []backend.Backend
	region   string
	version  uint64

	backendUpdated bool
	regionUpdated  bool
	versionUpdated bool
}

func (t *peerMetaTx) Backend(backends ...backend.Backend) {
	t.backendUpdated = true
	t.backends = backends
}

func (t *peerMetaTx) Region(region string) {
	t.regionUpdated = true
	t.region = region
}

func (t *peerMetaTx) Version(version uint64) {
	t.versionUpdated = true
	t.version = version
}

type PeerBackend struct {
	Type     pbp.PeerBackend_BackendType
	Priority uint32
	Endpoint string
}

type PeerMeta struct {
	lockState       sync.RWMutex
	stateVersion    uint32
	state           pbp.Peer_State
	lastStateUpdate time.Time

	lock              sync.RWMutex
	version           uint64
	region            string
	backendByPriority []*PeerBackend
	activeBackend     int
}

func (p *PeerMeta) Init(region string) {
	p.region = region
	p.stateVersion = 0
	p.lastStateUpdate = time.Now()
	p.version = 0
	p.activeBackend = 0
}

func (p *PeerMeta) State() (int, uint32, time.Time) {
	p.lockState.RLock()
	defer p.lockState.RUnlock()

	state, version, last := p.state, p.stateVersion, p.lastStateUpdate
	switch state {
	case pbp.Peer_ALIVE:
		state = gossip.ALIVE
	case pbp.Peer_DEAD:
		state = gossip.DEAD
	case pbp.Peer_SUSPECTED:
		state = gossip.SUSPECTED
	}
	return int(state), version, last
}

func (p *PeerMeta) SetState(state int, version uint32, lastUpdate time.Time) {
	p.lockState.Lock()
	defer p.lockState.Unlock()

	switch state {
	case gossip.ALIVE:
		p.state = pbp.Peer_ALIVE
	case gossip.DEAD:
		p.state = pbp.Peer_DEAD
	case gossip.SUSPECTED:
		p.state = pbp.Peer_SUSPECTED
	default:
		// p.log.Warn("unrecoginized gossip state: ", state)
		return
	}
	p.stateVersion = version
	p.lastStateUpdate = lastUpdate
}

func (p *PeerMeta) Region() string {
	return p.region
}

func (p *PeerMeta) String() string {
	return "state_ver:" + strconv.FormatUint(uint64(p.stateVersion), 10) +
		",ver:" + strconv.FormatUint(p.version, 10)
}

func (p *PeerMeta) PBSnapshot() (msgPeer *pbp.Peer, err error) {
	msgPeer = &pbp.Peer{
		Version:      p.version,
		StateVersion: p.stateVersion,
		State:        p.state,
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
		set[*p.backendByPriority[idx]] = struct{}{}
	}
	for idx := range backends {
		if _, exist := set[PeerBackend{
			Type:     backends[idx].Type(),
			Priority: backends[idx].Priority(),
			Endpoint: backends[idx].Publish(),
		}]; !exist {
			return false
		}
	}
	return true
}

func (p *PeerMeta) Tx(commit func(PeerReleaseTx) bool) {
	p.lock.Lock()
	defer p.lock.Unlock()

	tx, shouldCommit := &peerMetaTx{}, false
	shouldCommit = commit(tx) && ((tx.backendUpdated && p.backendEqual(tx.backends...)) ||
		(tx.regionUpdated && p.region != tx.region))

	if !shouldCommit || (tx.versionUpdated && p.version <= p.version) {
		return
	}

	// update version.
	if tx.versionUpdated {
		if p.version <= tx.version { // accept new version only.
			return
		}
		p.version = tx.version
	} else {
		// version should be monotonic increasing number over the cluster.
		// use unix nano as version since only publisher can create a new version.
		p.version = uint64(time.Now().UnixNano()) // new version.
	}

	// update region.
	if tx.regionUpdated {
		p.region = tx.region
	}

	// update backends.
	p.backendByPriority = make([]*PeerBackend, 0, len(tx.backends))
	for idx := range tx.backends {
		b := tx.backends[idx]
		if b == nil {
			continue
		}
		backend := &PeerBackend{
			Type:     b.Type(),
			Priority: b.Priority(),
			Endpoint: b.Publish(),
		}
		p.backendByPriority = append(p.backendByPriority, backend)
	}
	sort.Slice(p.backendByPriority, func(i, j int) bool {
		return p.backendByPriority[i].Priority > p.backendByPriority[j].Priority
	})
}
