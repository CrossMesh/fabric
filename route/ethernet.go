package route

import (
	"bytes"
	"sync"
	"time"

	"github.com/crossmesh/fabric/backend"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

var (
	EthernetBoardcastAddress = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

type L2Peer struct {
	PeerMeta
}

type L2Router struct {
	BaseRouter

	peerMAC sync.Map // map[*PeerMeta]map[[6]byte]struct{}
	byMAC   sync.Map // map[[6]byte]*hotMAC

	recordExpire time.Duration
	visitor      MembershipVistor
}

type hotMAC struct {
	p       MembershipPeer
	mac     [6]byte
	lastHit time.Time
}

func NewL2Router(arbiter *arbit.Arbiter, membership Membership, log *logging.Entry, recordExpire time.Duration) (r *L2Router) {
	if log == nil {
		log = logging.WithField("module", "l2_route")
	}
	r = &L2Router{
		BaseRouter: BaseRouter{
			log: log,
		},
		recordExpire: recordExpire,
	}
	membership.OnAppend(r.append)
	membership.OnRemove(r.remove)
	r.visitor = membership
	r.goTasks(arbiter)
	return
}

func (r *L2Router) getMACSet(p MembershipPeer) *sync.Map {
	v, ok := r.peerMAC.Load(p.Meta())
	if !ok || v == nil {
		return nil
	}
	m, _ := v.(*sync.Map)
	return m
}

func (r *L2Router) expireHotMAC(now time.Time) {
	r.byMAC.Range(func(k, v interface{}) bool {
		hot, isHotMAC := v.(*hotMAC)
		if !isHotMAC {
			return true
		}
		// expired.
		if hot.lastHit.Add(r.recordExpire).Before(now) {
			r.byMAC.Delete(k)
			if macSet := r.getMACSet(hot.p); macSet != nil {
				macSet.Delete(hot.mac)
			}
		}
		return true
	})
}

func (r *L2Router) goTasks(arbiter *arbit.Arbiter) {
	r.BaseRouter.goTasks(arbiter)
	// expire records.
	cacheCleanningDuration := time.Second
	if r.recordExpire < cacheCleanningDuration {
		cacheCleanningDuration = r.recordExpire
	}
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.expireHotMAC(time.Now())
	}, cacheCleanningDuration, 1)
}

func (r *L2Router) Forward(frame []byte) (peers []MembershipPeer) {
	var dst [6]byte

	if len(frame) < 14 {
		// frame too small
		return nil
	}
	copy(dst[:], frame[0:6])

	// lookup.
	if 0 != bytes.Compare(dst[:], EthernetBoardcastAddress[:]) { // not boardcase
		if dst[0]&0x01 != 0 { // multicast not supported now.
			return nil
		}
		v, hasPeer := r.byMAC.Load(dst)
		if hasPeer {
			hot, isHot := v.(*hotMAC)
			if isHot { // found.
				r.hitPeer(hot.p)
				hot.lastHit = time.Now()
				return []MembershipPeer{hot.p}
			}
		}
	}
	// fallback to boardcast.
	r.visitor.Range(func(p MembershipPeer) bool {
		// not boardcast myself.
		if p.Meta().IsSelf() {
			return true
		}
		peers = append(peers, p)
		return true
	})
	return
}

func (r *L2Router) Backward(frame []byte, backend backend.PeerBackendIdentity) (p MembershipPeer, new bool) {
	var src [6]byte

	// decode source MAC.
	if len(frame) < 14 {
		// frame too small
		return nil, false
	}
	copy(src[:], frame[6:12])
	if p = r.BackendPeer(backend); p == nil {
		return nil, false
	}
	if 0 == bytes.Compare(src[:], EthernetBoardcastAddress[:]) {
		// do not learn boardcast address.
		return p, false
	}
	new = true
	// lookup record by MAC.
	for {
		v, loaded := r.byMAC.Load(src)
		if loaded {
			hot, isHot := v.(*hotMAC)
			if isHot { // found.
				new = false
				r.hitPeer(p)

				// update record.
				if hot.p != p {
					// move mac to new set.
					if macs := r.getMACSet(hot.p); macs != nil {
						macs.Delete(src)
					}
					if macs := r.getMACSet(p); macs != nil {
						macs.Store(src, struct{}{})
					}
				}
				hot.p = p
				hot.lastHit = time.Now()
				break
			}
		}

		// learn mac.
		new := &hotMAC{
			p:       p,
			lastHit: time.Now(),
		}
		copy(new.mac[:], src[:])
		if v, loaded = r.byMAC.LoadOrStore(src, new); !loaded { // changed by me.
			macs := r.getMACSet(p)
			if macs != nil {
				macs.Store(src, struct{}{})
			}
		}
		break
	}
	return
}

func (r *L2Router) append(v MembershipPeer) {
	r.BaseRouter.append(v)
	// allocate MAC set.
	r.peerMAC.Store(v.Meta(), &sync.Map{})
}

func (r *L2Router) remove(v MembershipPeer) {
	r.BaseRouter.remove(v)

	// remove related records.
	macs := r.getMACSet(v)
	// deallocate MAC set.
	r.peerMAC.Delete(v.Meta())
	if macs != nil {
		macs.Range(func(k, v interface{}) bool {
			r.byMAC.Delete(k)
			return true
		})
		return
	}
}
