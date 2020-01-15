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

type Peer interface {
	State() (int, uint32, time.Time)
	SetState(int, uint32, time.Time)
	Region() string
}

type GossipContext struct {
	gossiper *Gossiper
	term     uint32
	peers    []Peer

	lock       sync.Mutex
	discovered []Peer
}

func (c *GossipContext) NumOfPeers() int { return len(c.peers) }

func (c *GossipContext) Peer(index int) Peer {
	if index > len(c.peers) {
		return nil
	}
	return c.peers[index]
}

// Indicate applies state read from remote peers.
func (c *GossipContext) Indicate(idx int, version uint32, state int) {
	// ignore invalid indications
	if idx > len(c.peers) || state > 2 {
		return
	}
	peer := c.peers[idx]
	localState, localVersion, _ := peer.State()
	// a dead peer cannot be back to live.
	if localState == DEAD {
		return
	}
	// always accept new state
	if state == DEAD || version > localVersion {
		peer.SetState(state, version, time.Now())
		return
	}
	// some peers suspect. so am I.
	if state == SUSPECTED && localState != SUSPECTED {
		peer.SetState(state, version, time.Now())
	}
}

// ClaimAlive should be called after an acknowledgement received from remote peer.
func (c *GossipContext) ClaimAlive(idx int) {
	if idx > len(c.peers) {
		return
	}
	peer := c.peers[idx]
	localState, localVersion, _ := peer.State()
	// a dead peer cannot be back to live.
	if localState == DEAD {
		return
	}
	// I learn that the suspection is wrong. Clarify it.
	if localState != ALIVE {
		peer.SetState(ALIVE, localVersion+1, time.Now())
	}
}

// ClaimDead should be called when an acknowledgement cannot received from remote peer.
func (c *GossipContext) ClaimDead(idx int) {
	if idx > len(c.peers) {
		return
	}
	peer := c.peers[idx]
	localState, localVersion, _ := peer.State()
	// a dead peer cannot be back to live.
	if localState == DEAD {
		return
	}
	// I suspect the peer is dead.
	if localState != SUSPECTED {
		peer.SetState(SUSPECTED, localVersion, time.Now())
	}
}

// Discover add new discovered peer.
func (c *GossipContext) Discover(peer Peer) {
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
	byRegion map[string][]Peer

	MinRegionPeers int
	SuspectTimeout time.Duration
}

func NewGossiper() (g *Gossiper) {
	g = &Gossiper{
		byRegion:       make(map[string][]Peer),
		MinRegionPeers: 1,
		SuspectTimeout: time.Minute * 5,
	}
	return
}

func (g *Gossiper) Do(brust int32, transport func(*GossipContext)) {
	if brust < 1 {
		brust = 1
	}

	gctx := &GossipContext{
		peers:    make([]Peer, 0, brust),
		gossiper: g,
	}
	gctx.term = atomic.AddUint32(&g.term, 1)

	g.lock.RLock()
	finishTransport := false
	// for recovering from transport() panic
	defer func() {
		if !finishTransport {
			g.lock.RUnlock()
		}
	}()

	// select random N peers
	// posibility proof:
	//	P = m/i * (1 - 1/(i+1)) * (1 - 1/(i+2)) * ... * (1 - 1/n) = m/i * i/(i+1) * (i+1)/(i+2) * ... * (n-1)/n = m/n
	idx := int32(0)
	g.visitPeer(func(region string, peer Peer) bool {
		if idx < brust {
			gctx.peers = append(gctx.peers, peer)
		} else if n := rand.Int31n(idx); n < brust {
			gctx.peers[n] = peer
		}
		idx++
		return true
	})

	transport(gctx)
	finishTransport = true

	g.lock.RUnlock()
	g.lock.Lock()
	defer g.lock.Lock()
	// add discovered member.
	if len(gctx.discovered) > 0 {
		for _, peer := range gctx.discovered {
			if peer != nil {
				g.addToRegion(peer.Region(), peer)
			}
		}
	}

	// clean dead peers.
	for region, peers := range g.byRegion {
		for idx, peer := range peers {
			if state, _, _ := peer.State(); state != DEAD || len(peers) <= g.MinRegionPeers {
				continue
			}

			// remove peer.
			peers[idx] = peers[len(peers)-1]
			peers = peers[:len(peers)-1]
			g.byRegion[region] = peers
		}
	}

}

func (g *Gossiper) addToRegion(region string, peer Peer) {
	if peer == nil {
		return
	}
	regionPeers, _ := g.byRegion[region]
	if regionPeers == nil {
		regionPeers = make([]Peer, 0, 1)
	}
	regionPeers = append(regionPeers, peer)
	g.byRegion[region] = regionPeers
	return
}

func (g *Gossiper) Seed(peers ...Peer) {
	g.lock.Lock()
	defer g.lock.Unlock()
	for _, peer := range peers {
		peer.SetState(SUSPECTED, 0, time.Now())
		g.addToRegion(peer.Region(), peer)
	}
}

func (g *Gossiper) visitPeer(visit func(string, Peer) bool) {
	for region, peers := range g.byRegion {
		for _, peer := range peers {
			if !visit(region, peer) {
				return
			}
		}
	}
}

func (g *Gossiper) VisitPeer(visit func(string, Peer) bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	g.visitPeer(visit)
}

func (g *Gossiper) visitPeerByState(visit func(Peer) bool, states ...int) {
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

func (g *Gossiper) VisitPeerByState(visit func(Peer) bool, states ...int) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	g.visitPeerByState(visit)
}
