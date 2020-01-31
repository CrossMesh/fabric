package gossip

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGossip(t *testing.T) {
	g := NewGossiper()
	g.MinRegionPeers = 1
	peers := []*Peer{
		{state: ALIVE, stateVersion: 1, region: "dc1", lastStateUpdate: time.Now()},
		{state: ALIVE, stateVersion: 2, region: "dc1", lastStateUpdate: time.Now()},
		{state: DEAD, stateVersion: 5, region: "dc1", lastStateUpdate: time.Now()},
		{state: ALIVE, stateVersion: 3, region: "dc2", lastStateUpdate: time.Now()},
		{state: SUSPECTED, stateVersion: 5, region: "dc3", lastStateUpdate: time.Now()},
		{state: DEAD, stateVersion: 5, region: "dc4", lastStateUpdate: time.Now()},
	}
	peerSet := map[*Peer]struct{}{}
	inPeerSet := func(p MembershipPeer) bool {
		if p, ok := p.(*Peer); !ok || p == nil {
			t.Error("GossipTerm.Peer() got invalid peer.")
			return false
		} else if _, inPeerSet := peerSet[p]; !inPeerSet {
			t.Error("GossipTerm.Peer() not in peer set.")
			return false
		}
		return true
	}
	appendCalledCount, removeCalledCount := 0, 0
	g.OnAppend(func(p MembershipPeer) {
		peerSet[p.(*Peer)] = struct{}{}
		appendCalledCount++
	}).OnRemove(func(p MembershipPeer) {
		delete(peerSet, p.(*Peer))
		removeCalledCount++
	})

	// seed.
	for _, peer := range peers {
		g.Discover(peer)
	}
	g.Discover(nil, nil)
	assert.Equal(t, uint32(0), g.term)
	peerCount := 0
	g.VisitPeer(func(region string, peer MembershipPeer) bool {
		assert.True(t, inPeerSet(peer))
		assert.Equal(t, peer.(*Peer).region, region)
		peerCount++
		return true
	})
	assert.Equal(t, len(peers), peerCount)
	assert.Equal(t, len(peers), appendCalledCount)

	// term.
	term := g.NewTerm(2)
	assert.Equal(t, uint32(1), term.ID)
	assert.Equal(t, uint32(1), term.ID)
	assert.Equal(t, 2, term.NumOfPeers())
	assert.Nil(t, term.Peer(3))
	assert.True(t, inPeerSet(term.Peer(0)))

	g.Clean(time.Now().Add(DefaultSuspectTimeout + time.Second))
	assert.Equal(t, 1, removeCalledCount)
	aliveCount, suspectCount, deadCount := 0, 0, 0
	g.VisitPeerByState(func(peer MembershipPeer) bool {
		assert.True(t, inPeerSet(peer))
		switch s, _, _ := peer.State(); s {
		case ALIVE:
			aliveCount++
		case SUSPECTED:
			suspectCount++
		case DEAD:
			deadCount++
		}
		return true
	}, ALIVE, SUSPECTED)
	assert.Equal(t, 3, aliveCount)
	assert.Equal(t, 0, suspectCount)

	// region change.
	peers[0].Tx(func(tx *PeerReleaseTx) bool {
		tx.Region("dc2")
		return true
	})
	g.VisitPeer(func(region string, peer MembershipPeer) bool {
		assert.True(t, inPeerSet(peer))
		assert.Equal(t, peer.(*Peer).region, region) // region should be consistent.
		return true
	})

	// zero peer to gossip.
	term = g.NewTerm(0)
	assert.Equal(t, uint32(2), term.ID)
	assert.Equal(t, 0, term.NumOfPeers())

	// discover.
	appendCalledCount, removeCalledCount = 0, 0
	g.Discover(&Peer{state: ALIVE, stateVersion: 2, region: "dc1"})
	assert.Equal(t, 1, appendCalledCount)
}

func TestPeer(t *testing.T) {
	(&Peer{}).RTx(func() {})
	t.Run("peer_init_state", func(t *testing.T) {
		p := &Peer{}
		// initial state.
		assert.Equal(t, "", p.region)
		assert.Equal(t, uint32(0), p.stateVersion)
		assert.Equal(t, ALIVE, p.state)
	})

	t.Run("tx_update_trace", func(t *testing.T) {
		p := &Peer{}

		// commit nothing.
		t.Run("empty", func(t *testing.T) {
			assert.Equal(t, false, p.Tx(func(tx *PeerReleaseTx) bool {
				assert.False(t, tx.stateUpdated)
				assert.False(t, tx.regionUpdated)
				assert.False(t, tx.ShouldCommit())
				return true
			}))
		})

		// commit region.
		t.Run("region", func(t *testing.T) {
			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.Region("dc0")
				assert.True(t, tx.regionUpdated)
				assert.Equal(t, "dc0", tx.region)
				assert.True(t, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, "dc0", p.region)
			assert.Equal(t, uint32(1), p.stateVersion)
		})

		// state and region.
		t.Run("state_and_region", func(t *testing.T) {
			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.Region("dc1-0")
				tx.State(ALIVE, 1)

				assert.Equal(t, true, tx.regionUpdated)
				assert.Equal(t, false, tx.stateUpdated)
				assert.Equal(t, "dc1-0", tx.region)
				assert.Equal(t, true, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, "dc1-0", p.region)
			assert.Equal(t, uint32(2), p.stateVersion)

			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.Region("dc1")
				tx.State(ALIVE, 3)

				assert.Equal(t, true, tx.regionUpdated)
				assert.Equal(t, true, tx.stateUpdated)
				assert.Equal(t, "dc1", tx.region)
				assert.Equal(t, true, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, "dc1", p.region)
			assert.Equal(t, uint32(3), p.stateVersion)

		})

		// commit state.
		t.Run("state_apply", func(t *testing.T) {
			// test: invalid state
			assert.Equal(t, false, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.State(101201, 100)
				assert.Equal(t, false, tx.stateUpdated)
				assert.Equal(t, false, tx.ShouldCommit())
				return true
			}))

			// test: suspected
			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.State(SUSPECTED, 3)
				assert.Equal(t, true, tx.stateUpdated)
				assert.Equal(t, true, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, uint32(3), p.stateVersion)
			// some peers suspect. so am I.
			assert.Equal(t, SUSPECTED, p.state)

			// do not accept old version.
			assert.Equal(t, false, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.State(ALIVE, 0)
				assert.Equal(t, false, tx.stateUpdated)
				assert.Equal(t, false, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, uint32(3), p.stateVersion)
			assert.Equal(t, SUSPECTED, p.state)

			assert.Equal(t, false, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.State(ALIVE, 3)
				assert.Equal(t, false, tx.stateUpdated)
				assert.Equal(t, false, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, uint32(3), p.stateVersion)
			assert.Equal(t, SUSPECTED, p.state)

			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.State(ALIVE, 4)
				tx.Region("dc1")

				assert.Equal(t, true, tx.stateUpdated)
				assert.Equal(t, false, tx.regionUpdated)
				assert.Equal(t, true, tx.ShouldCommit())
				return true
			}))
			assert.Equal(t, uint32(4), p.stateVersion)
			assert.Equal(t, ALIVE, p.state)
		})

		t.Run("state_claim", func(t *testing.T) {
			// claim dead.
			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.ClaimDead()

				assert.Equal(t, true, tx.stateUpdated)
				assert.Equal(t, false, tx.regionUpdated)
				assert.Equal(t, p.stateVersion, tx.stateVersion)
				assert.Equal(t, SUSPECTED, tx.state)
				return true
			}))
			assert.Equal(t, uint32(4), p.stateVersion)
			assert.Equal(t, SUSPECTED, p.state)

			// clearify.
			assert.Equal(t, true, p.Tx(func(tx *PeerReleaseTx) bool {
				tx.ClaimAlive()

				assert.Equal(t, true, tx.stateUpdated)
				assert.Equal(t, false, tx.regionUpdated)
				assert.Equal(t, p.stateVersion+1, tx.stateVersion)
				assert.Equal(t, ALIVE, tx.state)
				return true
			}))
			assert.Equal(t, uint32(5), p.stateVersion)
			assert.Equal(t, ALIVE, p.state)
		})
	})
}
