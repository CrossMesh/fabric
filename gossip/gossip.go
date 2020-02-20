package gossip

import (
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ALIVE     = 0
	SUSPECTED = 1
	DEAD      = 2

	DefaultGossipPeriod   = time.Second * 5
	DefaultSuspectTimeout = time.Minute * 5
)

// StateName map state to it's name.
var StateName = map[int]string{
	ALIVE:     "alive",
	SUSPECTED: "suspected",
	DEAD:      "dead",
}

// MembershipPeer is gossip peer abstraction.
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

// ShouldCommit tells whether to change state of peer.
func (t *PeerReleaseTx) ShouldCommit() bool {
	return t.regionUpdated || t.stateUpdated
}

// Peer is minimum implement for MembershipPeer abstraction.
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
func (p *Peer) Reset()                          { p.stateVersion = 0 }
func (p *Peer) String() string {
	stateName, _ := StateName[p.state]
	if stateName == "" {
		stateName = "unknown"
	}
	return "liveness(" + stateName + "," + strconv.FormatInt(int64(p.stateVersion), 10) + ",\"" + p.region + "\")"
}

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

// RTx begins new read transaction.
func (p *Peer) RTx(commit func()) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	commit()
}

// GossipTerm contains details in gossip term.
type GossipTerm struct {
	gossiper *Gossiper
	ID       uint32
	peers    []MembershipPeer
}

// NumOfPeers returns the number of peers that should be gossip with.
func (c *GossipTerm) NumOfPeers() int { return len(c.peers) }

// Peer returns gossip peer by index.
func (c *GossipTerm) Peer(index int) MembershipPeer {
	if index > len(c.peers) {
		return nil
	}
	return c.peers[index]
}

// Gossiper implements basic gossip protocol.
type Gossiper struct {
	term     uint32
	onRemove func(MembershipPeer)
	onAppend func(MembershipPeer)

	minRegionPeers int
	SuspectTimeout time.Duration

	lock     sync.RWMutex
	byRegion map[string]map[*Peer]MembershipPeer
}

// NewGossiper creates new gossiper.
func NewGossiper() (g *Gossiper) {
	g = &Gossiper{
		byRegion:       make(map[string]map[*Peer]MembershipPeer),
		minRegionPeers: 2,
		SuspectTimeout: DefaultSuspectTimeout,
	}
	return
}

// MinRegionPeers returns the minimum number of peers to maintain for a region.
func (g *Gossiper) MinRegionPeers() int {
	return g.minRegionPeers
}

// SetMinRegionPeers sets the minimum number of peers to maintain for a region.
func (g *Gossiper) SetMinRegionPeers(n int) {
	if n < 1 {
		n = 1
	}
	g.minRegionPeers = n
}

// NewTerm creates new gossip term.
func (g *Gossiper) NewTerm(brust int32) (t *GossipTerm) {
	t = &GossipTerm{
		peers:    make([]MembershipPeer, 0, brust),
		gossiper: g,
	}
	t.ID = atomic.AddUint32(&g.term, 1)
	if brust > 0 {
		// select random N peers
		// posibility proof:
		//	P = m/i * (1 - 1/(i+1)) * (1 - 1/(i+2)) * ... * (1 - 1/n) = m/i * i/(i+1) * (i+1)/(i+2) * ... * (n-1)/n = m/n
		idx := int32(0)
		g.visitPeer(func(region string, peer MembershipPeer) bool {
			if peer.IsSelf() {
				return true
			}
			if idx < brust {
				t.peers = append(t.peers, peer)
			} else if n := rand.Int31n(idx); n < brust {
				t.peers[n] = peer
			}
			idx++
			return true
		})
	}
	return
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
	g.getRegionSet(region)[peer.GossiperStub()] = peer
}

func (g *Gossiper) refreshRegion(old, new string, peer MembershipPeer) {
	g.lock.Lock()
	defer g.lock.Unlock()

	regionPeers := g.getRegionSet(old)
	oldRef := regionPeers[peer.GossiperStub()]
	delete(regionPeers, peer.GossiperStub())
	regionPeers = g.getRegionSet(new)
	regionPeers[peer.GossiperStub()] = oldRef
}

// Clean updates states of peers and clean dead ones.
func (g *Gossiper) Clean(now time.Time) {
	g.lock.Lock()
	defer g.lock.Unlock()

	onRemove := g.onRemove
	for _, peers := range g.byRegion {
		for _, peer := range peers {
			state, _, last := peer.State()
			switch state {
			case DEAD:
				// clean dead peers.
				if len(peers) <= g.minRegionPeers {
					continue
				}
				// do not remove myself.
				if peer.IsSelf() {
					continue
				}
				// remove peer.
				if onRemove != nil {
					onRemove(peer)
				}
				delete(peers, peer.GossiperStub())
				peer.GossiperStub().g = nil

			case SUSPECTED:
				if last.Add(g.SuspectTimeout).After(now) {
					continue
				}
				peer.GossiperStub().state = DEAD
			}
		}
	}
}

// Discover applies new peers.
func (g *Gossiper) Discover(peers ...MembershipPeer) {
	g.lock.Lock()
	defer g.lock.Unlock()
	onAppend := g.onAppend
	if onAppend == nil {
		onAppend = func(MembershipPeer) {}
	}

	for _, peer := range peers {
		if peer == nil {
			continue
		}
		g.addToRegion(peer.Region(), peer)
		peer.GossiperStub().g = g
		onAppend(peer)
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

// VisitPeer visits peers in memberlist.
func (g *Gossiper) VisitPeer(visit func(string, MembershipPeer) bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()

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

// VisitPeerByState visits peers in memberlist by states.
func (g *Gossiper) VisitPeerByState(visit func(MembershipPeer) bool, states ...int) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	g.visitPeerByState(visit, states...)
}

// OnRemove inserts callback to receive peer "remove" event.
func (g *Gossiper) OnRemove(callback func(MembershipPeer)) *Gossiper { g.onRemove = callback; return g }

// OnAppend inserts callback to receive peer "append" event.
func (g *Gossiper) OnAppend(callback func(MembershipPeer)) *Gossiper { g.onAppend = callback; return g }
