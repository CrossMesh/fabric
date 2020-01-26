package route

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/gossip"
	pbp "git.uestc.cn/sunmxt/utt/proto/pb"
	logging "github.com/sirupsen/logrus"
)

type L3Router struct {
	BaseRouter

	peerIP sync.Map // map[*gossip.Peer]map[[4]byte]struct{}
	byIP   sync.Map // map[[4]byte]*hotIP

	recordExpire time.Duration
}

func NewL3Router(arbiter *arbit.Arbiter, log *logging.Entry, recordExpire time.Duration) (r *L2Router) {
	if log == nil {
		log = logging.WithField("module", "l3_router")
	}
	r = &L2Router{}
	r.BaseRouter.gossip = NewGossipGroup()
	r.gossip.OnAppend(r.append).OnRemove(r.remove)
	return
}

type hotIP struct {
	p       Peer
	IP      [4]byte
	lastHit time.Time
}

func (r *L3Router) goTasks(arbiter *arbit.Arbiter) {
	r.BaseRouter.goTasks(arbiter)

	// expire records.
	expiredTimeout := time.Second
	if r.recordExpire < expiredTimeout {
		expiredTimeout = r.recordExpire
	}
	arbiter.TickGo(func(cancel func(), deadline time.Time) {
		r.byIP.Range(func(k, v interface{}) bool {
			hot, isHot := k.(*hotIP)
			if !isHot {
				return true
			}
			// exipred.
			if hot.lastHit.Add(r.recordExpire).Before(r.now) {
				r.byIP.Delete(k)
				if ips := r.getIPSet(hot.p); ips != nil {
					ips.Delete(hot.IP)
				}
			}
			return true
		})
	}, expiredTimeout, 1)
}

func (r *L3Router) getIPSet(p gossip.MembershipPeer) *sync.Map {
	v, ok := r.peerIP.Load(p.GossiperStub())
	if !ok || v == nil {
		return nil
	}
	m, _ := v.(*sync.Map)
	return m
}

func (r *L3Router) Forward(packet []byte) (peers []Peer) {
	var dst [4]byte
	if len(packet) < 20 {
		// packet too small.
		return
	}
	if version := uint8(packet[0]) >> 4; version != 4 {
		// not version 4.
		return
	}
	copy(dst[:], packet[16:20])
	ip := net.IP(dst[0:4])
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		// drop lookback and unspecified address.
		// multicast is not supported now.
		return
	}

	// lookup.
	if !ip.Equal(net.IPv4bcast) { // unicast.
		v, hasPeer := r.byIP.Load(dst)
		if hasPeer {
			hot, isHot := v.(*hotIP)
			if !isHot { // found.
				r.hitPeer(hot.p)
				hot.lastHit = r.now
				return []Peer{hot.p}
			}
		}
	}
	r.gossip.VisitPeer(func(region string, p gossip.MembershipPeer) bool {
		if l3, ok := p.(*L3Peer); ok {
			if l3.Net.Contains(ip) {
				// route by subnet.
				peers = append(peers, Peer(l3))
				return true
			}
		} else if peer, ok := p.(Peer); ok {
			// fallback to boardcast.
			peers = append(peers, peer)
		}
		return true
	})
	return
}

func (r *L3Router) Backward(packet []byte, backend PeerBackend) (p Peer) {
	var src [6]byte

	if p = r.BackendPeer(backend); p == nil {
		return
	}
	if len(packet) < 20 {
		// packet too small.
		return
	}
	if version := uint8(packet[0]) >> 4; version != 4 {
		// not version 4.
		return
	}
	copy(src[:], packet[12:16])
	ip := net.IP(src[0:4])
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		// drop lookback and unspecified address.
		// multicast is not supported now.
		return
	}
	if !ip.Equal((net.IPv4bcast)) {
		return
	}
	// lookup record.
	for {
		v, loaded := r.byIP.Load(src)
		if loaded {
			hot, isHot := v.(*hotIP)
			if isHot {
				r.hitPeer(hot.p)

				// update record.
				hot.lastHit = r.now
				hot.p = p
				break
			}
		}

		// learn
		new := &hotIP{
			p:       p,
			lastHit: r.now,
		}
		copy(new.IP[:], src[:])
		if v, loaded = r.byIP.LoadOrStore(src, new); !loaded { // changed by me.
			ips := r.getIPSet(p)
			if ips != nil {
				ips.Store(src, struct{}{})
			}
		}
	}

	return nil
}

func (r *L3Router) append(v gossip.MembershipPeer) {
	r.BaseRouter.append(v)
	// allocate
	r.peerIP.Store(v.GossiperStub(), &sync.Map{})
}

func (r *L3Router) remove(v gossip.MembershipPeer) {
	r.BaseRouter.remove(v)

	// remove related records.
	ips := r.getIPSet(v)
	// deallocate.
	r.peerIP.Delete(v.GossiperStub())
	if ips != nil {
		ips.Range(func(k, v interface{}) bool {
			r.byIP.Delete(k)
			return true
		})
	}
}

type L3Peer struct {
	PeerMeta

	Net net.IPNet
}

func (p *L3Peer) Valid() bool { return p.Net.IP.IsGlobalUnicast() }

func (p *L3Peer) String() string {
	return "l3(" + p.Net.IP.String() + "," + p.PeerMeta.String() + ")"
}

func (p *L3Peer) PBSnapshot() (msgPeer *pbp.Peer, err error) {
	msgPeer, err = p.PeerMeta.PBSnapshot()

	vip, vipLen := binary.Uvarint([]byte(p.Net.IP.To4()))
	if vipLen != 4 {
		// IPv4 should should not hit this.
		return nil, fmt.Errorf("bug: virtual ip address is of %v bytes length", vipLen)
	}
	msgPeer.VirtualIp = uint32(vip)
	maskOnes, maskLen := p.Net.Mask.Size()
	if maskLen != 32 {
		// IPv4 should should not hit this.
		return nil, fmt.Errorf("bug: virtual ip mask is of %v bits length", maskLen)
	}
	msgPeer.VirtualMask = uint32(maskOnes)

	return
}
