package route

import (
	"sync"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/gossip"
	logging "github.com/sirupsen/logrus"
)

type L2Peer struct {
	PeerMeta
}

type L2Router struct {
	BaseRouter

	gossip *GossipGroup

	lock    sync.RWMutex
	peerMAC sync.Map // map[*gossip.Peer]map[[6]byte]struct{}
	byMAC   sync.Map // map[[6]byte]*hotMAC

	recordExpire time.Duration
}

type hotMAC struct {
	p       Peer
	mac     [6]byte
	lastHit time.Time
}

func NewL2Router(arbiter *arbit.Arbiter, log *logging.Entry, recordExpire time.Duration) (r *L2Router) {
	if log == nil {
		log = logging.WithField("module", "l2_router")
	}
	r = &L2Router{
		gossip: NewGossipGroup(),
	}
	r.gossip.OnAppend(r.append)
	r.gossip.OnRemove(r.remove)
	return
}

func (r *L2Router) getMACSet(p gossip.MembershipPeer) *sync.Map {
	v, ok := r.peerMAC.Load(p.GossiperStub())
	if !ok || v == nil {
		return nil
	}
	m, _ := v.(*sync.Map)
	return m
}

func (r *L2Router) goTasks(arbiter *arbit.Arbiter) {
	r.BaseRouter.goTasks(arbiter)

	// expire records.
	cacheCleanningDuration := time.Second
	if r.recordExpire < cacheCleanningDuration {
		cacheCleanningDuration = r.recordExpire
	}
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.byMAC.Range(func(k, v interface{}) bool {
			hot, isHotMAC := k.(*hotMAC)
			if !isHotMAC {
				return true
			}
			// expired.
			if hot.lastHit.Add(r.recordExpire).Before(r.now) {
				r.byMAC.Delete(k)
				if macSet := r.getMACSet(hot.p); macSet != nil {
					macSet.Delete(hot.mac)
				}
			}
			return true
		})
	}, cacheCleanningDuration, 1)
}

func (r *L2Router) Forward(frame []byte) (peers []Peer) {
	var dst [6]byte

	if len(frame) < 14 {
		// frame too small
		return nil
	}
	copy(dst[:], frame[0:6])

	// lookup.
	v, hasPeer := r.byMAC.Load(dst)
	if hasPeer {
		hot, isHot := v.(*hotMAC)
		if !isHot { // found.
			r.hitPeer(hot.p)
			hot.lastHit = r.now
			return []Peer{hot.p}
		}
	}
	// peer not found. boardcast to all peers.
	r.gossip.VisitPeer(func(region string, p gossip.MembershipPeer) bool {
		peer, isPeer := p.(Peer)
		if !isPeer {
			return true
		}
		peers = append(peers, peer)
		return true
	})
	return
}

func (r *L2Router) Backward(frame []byte, backend PeerBackend) (p Peer) {
	var src [6]byte

	// decode source MAC.
	if len(frame) < 14 {
		// frame too small
		return nil
	}
	copy(src[:], frame[6:12])
	if p = r.BackendPeer(backend); p == nil {
		return
	}
	// lookup record by MAC.
	v, loaded := r.byMAC.Load(src)
	for {
		if loaded {
			hot, isHot := v.(*hotMAC)
			if !isHot { // found.
				r.hitPeer(hot.p)

				// update record.
				hot.p = p
				hot.lastHit = r.now
				break
			}
		}

		// learn mac.
		new := &hotMAC{
			p:       p,
			lastHit: r.now,
		}
		copy(new.mac[:], src[:])
		if v, loaded = r.byMAC.LoadOrStore(src, new); !loaded {
			macs := r.getMACSet(p)
			if macs != nil { // changed by me.
				macs.Store(src, struct{}{})
			}
		}
		break
	}
	return
}

func (r *L2Router) append(v gossip.MembershipPeer) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.BaseRouter.append(v)
	// allocate MAC set.
	r.peerMAC.Store(v.GossiperStub(), &sync.Map{})
}

func (r *L2Router) remove(v gossip.MembershipPeer) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.BaseRouter.remove(v)

	// remove related records.
	macs := r.getMACSet(v)
	if macs != nil {
		macs.Range(func(k, v interface{}) bool {
			r.byMAC.Delete(k)
			return true
		})
		return
	}
	// deallocate MAC set.
	r.peerMAC.Delete(v.GossiperStub())
}
