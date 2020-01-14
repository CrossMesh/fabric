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
	// I learn that the suspection is wrong.
	if localState != ALIVE {
		peer.SetState(ALIVE, localVersion+1, time.Now())
	}
}

// ClaimDead should be called whtn an acknowledgement cannot received from remote peer.
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
	lock  sync.RWMutex
	peers []Peer
	term  uint32
}

func (g *Gossiper) Do(brust int32, transport func(*GossipContext)) {
	if brust < 1 {
		brust = 1
	}
	g.lock.RLock()
	defer g.lock.RUnlock()

	gctx := &GossipContext{
		peers:    make([]Peer, 0, brust),
		gossiper: g,
	}
	gctx.term = atomic.AddUint32(&g.term, 1)

	// select random N peers
	// posibility proof:
	//	P = m/i * (1 - 1/(i+1)) * (1 - 1/(i+2)) * ... * (1 - 1/n) = m/i * i/(i+1) * (i+1)/(i+2) * ... * (n-1)/n = m/n
	idx := int32(0)
	for _, peer := range g.peers {
		if idx < brust {
			gctx.peers = append(gctx.peers, peer)
		} else if n := rand.Int31n(idx); n < brust {
			gctx.peers[n] = peer
		}
	}

	transport(gctx)
}

func (g *Gossiper) Seed(peers ...Peer) {
	g.lock.Lock()
	defer g.lock.Unlock()
	for _, peer := range peers {
		peer.SetState(SUSPECTED, 0, time.Now())
	}
	g.peers = append(g.peers, peers...)
}
