package gossip

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ALIVE     = 0
	SUSPECTED = 1
	DEAD      = 2
)

var StateName = map[int]string{
	ALIVE:     "alive",
	SUSPECTED: "suspected",
	DEAD:      "dead",
}

type MembershipPeer interface {
	GossiperStub() *Peer

	State() (int, uint32, time.Time)
	Region() string
	IsSelf() bool
	Valid() bool
}

type PeerReleaseTx struct {
	p *Peer

	stateUpdated  bool
	regionUpdated bool

	state        int
	stateVersion uint32
	region       string
}

// Region update region.
func (t *PeerReleaseTx) Region(region string) {
	if region == t.p.region {
		return
	}
	t.regionUpdated, t.region = true, region
}

// State applies state read from remote peers.
func (t *PeerReleaseTx) State(state int, stateVersion uint32) {
	if state > 2 {
		return
	}
	p := t.p
	if p.state == DEAD || // a dead peer cannot be back to live.
		stateVersion < p.stateVersion { // ignore old version.
		return
	}
	if stateVersion == p.stateVersion {
		if p.state != ALIVE {
			return
		}
		if state == ALIVE {
			return
		}
	}
	// some peers suspect. so am I.
	t.stateUpdated, t.stateVersion, t.state = true, stateVersion, state
}

// ClaimAlive should be called after an acknowledgement received from remote peer.
func (t *PeerReleaseTx) ClaimAlive() {
	p := t.p
	// a dead peer cannot be back to live.
	if p.state == DEAD {
		return
	}
	// I learn that the suspection is wrong. Clarify it.
	if p.state != ALIVE {
		t.stateUpdated, t.stateVersion, t.state = true, p.stateVersion+1, ALIVE
	}
}

// ClaimDead should be called when an acknowledgement cannot received from remote peer.
func (t *PeerReleaseTx) ClaimDead() {
	p := t.p
	// a dead peer cannot be back to live.
	if p.state == DEAD {
		return
	}
	// I suspect the peer is dead.
	if p.state != SUSPECTED {
		t.stateUpdated, t.stateVersion, t.state = true, p.stateVersion, SUSPECTED
	}
}

func (t *PeerReleaseTx) ShouldCommit() bool {
	return t.regionUpdated || t.stateUpdated
}

type Peer struct {
	lock            sync.RWMutex
	g               *Gossiper
	stateVersion    uint32
	state           int
	region          string
	lastStateUpdate time.Time
}

func (p *Peer) State() (int, uint32, time.Time) { return p.state, p.stateVersion, p.lastStateUpdate }
func (p *Peer) Region() string                  { return p.region }
func (p *Peer) IsSelf() bool                    { return false }
func (p *Peer) Valid() bool                     { return true }
func (p *Peer) GossiperStub() *Peer             { return p }

func (p *Peer) Tx(commit func(*PeerReleaseTx) bool) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	tx, shouldCommit := &PeerReleaseTx{p: p}, false
	shouldCommit = commit(tx) && tx.ShouldCommit()
	if !shouldCommit {
		return false
	}
	if tx.regionUpdated && !tx.stateUpdated {
		tx.stateUpdated, tx.stateVersion, tx.state = true, p.stateVersion+1, p.state
	}
	if tx.regionUpdated {
		if p.g != nil {
			p.g.refreshRegion(p.region, tx.region, p)
		}
		p.region = tx.region
	}
	if tx.stateUpdated {
		p.state, p.stateVersion = tx.state, tx.stateVersion
		p.lastStateUpdate = time.Now()
	}

	return true
}

func (p *Peer) RTx(commit func()) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	commit()
}

type GossipContext struct {
	gossiper *Gossiper
	term     uint32
	peers    []MembershipPeer

	lock       sync.Mutex
	discovered []MembershipPeer
}

func (c *GossipContext) NumOfPeers() int { return len(c.peers) }

func (c *GossipContext) Peer(index int) MembershipPeer {
	if index > len(c.peers) {
		return nil
	}
	return c.peers[index]
}

func (c *GossipContext) VisitPeer(visit func(string, MembershipPeer) bool) {
	c.gossiper.visitPeer(visit)
}

func (c *GossipContext) visitPeerByState(visit func(MembershipPeer) bool, states ...int) {
	c.gossiper.visitPeerByState(visit, states...)
}

// Discover add new discovered peer.
func (c *GossipContext) Discover(peer MembershipPeer) {
	if peer == nil {
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	c.discovered = append(c.discovered, peer)
}

func (c *GossipContext) Term() uint32 { return c.term }

type Gossiper struct {
	lock     sync.RWMutex
	term     uint32
	newSeeds []MembershipPeer

	memberLock sync.RWMutex
	byRegion   map[string]map[*Peer]MembershipPeer

	onRemove func(MembershipPeer)
	onAppend func(MembershipPeer)

	MinRegionPeers int
	SuspectTimeout time.Duration
}

func NewGossiper() (g *Gossiper) {
	g = &Gossiper{
		byRegion:       make(map[string]map[*Peer]MembershipPeer),
		MinRegionPeers: 1,
		SuspectTimeout: time.Minute * 5,
	}
	return
}

func (g *Gossiper) applyNewSeeds() {
	for _, seed := range g.newSeeds {
		g.addToRegion(seed.Region(), seed)
		seed.GossiperStub().g = g
		if onAppend := g.onAppend; onAppend != nil {
			onAppend(seed)
		}
	}
	g.newSeeds = nil
}

func (g *Gossiper) Do(brust int32, transport func(*GossipContext)) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.applyNewSeeds()

	gctx := &GossipContext{
		peers:    make([]MembershipPeer, 0, brust),
		gossiper: g,
	}
	gctx.term = atomic.AddUint32(&g.term, 1)

	if brust > 0 {
		// select random N peers
		// posibility proof:
		//	P = m/i * (1 - 1/(i+1)) * (1 - 1/(i+2)) * ... * (1 - 1/n) = m/i * i/(i+1) * (i+1)/(i+2) * ... * (n-1)/n = m/n
		idx := int32(0)
		g.visitPeer(func(region string, peer MembershipPeer) bool {
			if peer.IsSelf() || !peer.Valid() {
				return true
			}
			if idx < brust {
				gctx.peers = append(gctx.peers, peer)
			} else if n := rand.Int31n(idx); n < brust {
				gctx.peers[n] = peer
			}
			idx++
			return true
		})
	}

	transport(gctx)
	onRemove, onAppend := g.onRemove, g.onAppend

	// add discovered member.
	if len(gctx.discovered) > 0 {
		for _, peer := range gctx.discovered {
			if peer != nil {
				if onAppend != nil {
					onAppend(peer)
				}
				g.addToRegion(peer.Region(), peer)
			}
		}
	}

	// clean dead peers.
	for _, peers := range g.byRegion {
		for _, peer := range peers {
			if state, _, _ := peer.State(); state != DEAD || len(peers) <= g.MinRegionPeers {
				continue
			}

			// remove peer.
			if onRemove != nil {
				onRemove(peer)
			}
			delete(peers, peer.GossiperStub())
			peer.GossiperStub().g = nil
		}
	}
}

func (g *Gossiper) getRegionSet(region string) (regionPeers map[*Peer]MembershipPeer) {
	regionPeers, _ = g.byRegion[region]
	if regionPeers == nil {
		regionPeers = make(map[*Peer]MembershipPeer)
		g.byRegion[region] = regionPeers
	}
	return
}

func (g *Gossiper) addToRegion(region string, peer MembershipPeer) {
	if peer == nil {
		return
	}
	g.memberLock.Lock()
	defer g.memberLock.Unlock()

	g.getRegionSet(region)[peer.GossiperStub()] = peer
}

func (g *Gossiper) refreshRegion(old, new string, peer MembershipPeer) {
	g.memberLock.Lock()
	defer g.memberLock.Unlock()

	regionPeers := g.getRegionSet(old)
	delete(regionPeers, peer.GossiperStub())
	regionPeers = g.getRegionSet(new)
	regionPeers[peer.GossiperStub()] = peer
}

func (g *Gossiper) Seed(peers ...MembershipPeer) {
	g.lock.Lock()
	defer g.lock.Unlock()

	for _, peer := range peers {
		if peer == nil {
			continue
		}
		g.newSeeds = append(g.newSeeds, peer)
	}
}

func (g *Gossiper) visitPeer(visit func(string, MembershipPeer) bool) {
	for region, peers := range g.byRegion {
		for _, peer := range peers {
			if !visit(region, peer) {
				return
			}
		}
	}
}

func (g *Gossiper) VisitPeer(visit func(string, MembershipPeer) bool) {
	g.memberLock.RLock()
	defer g.memberLock.RUnlock()

	g.visitPeer(visit)
}

func (g *Gossiper) visitPeerByState(visit func(MembershipPeer) bool, states ...int) {
	for _, peers := range g.byRegion {
		for _, peer := range peers {
			state, _, _ := peer.State()
			for _, targetState := range states {
				if targetState == state {
					if !visit(peer) {
						return
					}
					break
				}
			}
		}
	}
}

func (g *Gossiper) VisitPeerByState(visit func(MembershipPeer) bool, states ...int) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	g.visitPeerByState(visit, states...)
}

func (g *Gossiper) OnRemove(callback func(MembershipPeer)) *Gossiper { g.onRemove = callback; return g }

func (g *Gossiper) OnAppend(callback func(MembershipPeer)) *Gossiper { g.onAppend = callback; return g }
