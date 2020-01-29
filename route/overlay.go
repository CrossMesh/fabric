package route

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/gossip"
	pbp "git.uestc.cn/sunmxt/utt/proto/pb"
	"github.com/golang/protobuf/ptypes"
	logging "github.com/sirupsen/logrus"
)

// L3PeerReleaseTx is implement of transaction.
type L3PeerReleaseTx struct {
	*PeerReleaseTx

	p *L3Peer

	ip       net.IP
	mask     net.IPMask
	isRouter bool

	isRouterUpdated bool
}

func (t *L3PeerReleaseTx) IsRouter(isRouter bool) {
	if isRouter == t.p.isRouter {
		t.isRouterUpdated, t.isRouter = false, t.p.isRouter
		return
	}
	t.isRouterUpdated, t.isRouter = true, isRouter
}

func (t *L3PeerReleaseTx) CIDR(cidr string) error {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.mask, t.ip = nil, nil
		return err
	}
	t.Net(ip, n.Mask)
	return nil
}

func (t *L3PeerReleaseTx) Net(ip net.IP, mask net.IPMask) {
	ip = ip.To4()
	if ip == nil || mask == nil {
		return
	}
	if ip.Equal(t.p.ip) && 0 == bytes.Compare(mask, t.p.mask) { // no need to update.
		t.mask, t.ip = nil, nil
		return
	}
	t.ip, t.mask = ip, mask
}

func (t *L3PeerReleaseTx) ShouldCommit() bool {
	return t.PeerReleaseTx.ShouldCommit() || t.ip != nil || t.mask != nil || t.isRouterUpdated
}

type L3Router struct {
	BaseRouter

	peerIP sync.Map // map[*gossip.Peer]map[[4]byte]struct{}
	byIP   sync.Map // map[[4]byte]*hotIP

	recordExpire time.Duration
}

func NewL3Router(arbiter *arbit.Arbiter, log *logging.Entry, recordExpire time.Duration) (r *L3Router) {
	if log == nil {
		log = logging.WithField("module", "l3_router")
	}
	r = &L3Router{}
	r.BaseRouter.gossip = NewGossipGroup()
	r.gossip.OnAppend(r.append).OnRemove(r.remove)
	r.goTasks(arbiter)
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
			if isHot { // found.
				r.hitPeer(hot.p)
				hot.lastHit = r.now
				return []Peer{hot.p}
			}
		}
	}
	network := net.IPNet{
		IP: []byte{0, 0, 0, 0},
	}
	r.gossip.VisitPeer(func(region string, p gossip.MembershipPeer) bool {
		if l3, ok := p.(*L3Peer); ok {
			network.Mask = l3.mask
			if len(l3.ip) == len(network.Mask) {
				for i := range network.IP {
					network.IP[i] = l3.ip[i] & network.Mask[i]
				}
				if network.Contains(ip) && l3.isRouter {
					// route by subnet.
					peers = append(peers, Peer(l3))
				}
			}
		}
		return true
	})
	// select one.
	if len(peers) > 1 {
		idx := (r.now.UnixNano() / 1000000) % int64(len(peers))
		peers[0] = peers[idx]
		peers = peers[:1]
	}

	return
}

func (r *L3Router) Backward(packet []byte, backend backend.PeerBackendIdentity) (p Peer, new bool) {
	var src [4]byte

	if p = r.BackendPeer(backend); p == nil {
		return nil, false
	}
	if len(packet) < 20 {
		// packet too small.
		return p, false
	}
	if version := uint8(packet[0]) >> 4; version != 4 {
		// not version 4.
		return p, false
	}
	copy(src[:], packet[12:16])
	ip := net.IP(src[0:4])
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		// drop lookback and unspecified address.
		// multicast is not supported now.
		return p, false
	}
	if ip.Equal(net.IPv4bcast) {
		return p, false
	}
	// lookup record.
	new = true
	for {
		v, loaded := r.byIP.Load(src)
		if loaded {
			hot, isHot := v.(*hotIP)
			if isHot {
				new = false
				r.hitPeer(p)

				// update record.
				if hot.p != p {
					// move IP to new set.
					if ips := r.getIPSet(hot.p); ips != nil {
						ips.Delete(src)
					}
					if ips := r.getIPSet(p); ips != nil {
						ips.Store(src, struct{}{})
					}
				}

				hot.p = p
				hot.lastHit = r.now
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
		break
	}
	return
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

	ip   net.IP
	mask net.IPMask

	isRouter bool
}

func (p *L3Peer) Valid() bool { return p.ip.IsGlobalUnicast() }

func (p *L3Peer) String() string {
	return "l3(" + (&net.IPNet{
		IP:   p.ip,
		Mask: p.mask,
	}).String() + "," + p.PeerMeta.String() + ")"
}

func (p *L3Peer) PBSnapshot() (msgPeer *pbp.Peer, err error) {
	msgPeer, err = p.PeerMeta.PBSnapshot()
	msgOverlay := &pbp.OverlayPeer{}

	rawIP := []byte(p.ip.To4())
	if len(rawIP) != 4 {
		// IPv4 should should not hit this.
		return nil, fmt.Errorf("bug: virtual ip address is of %v bytes length", len(rawIP))
	}
	msgOverlay.VirtualIp = binary.BigEndian.Uint32(rawIP)
	maskOnes, maskLen := p.mask.Size()
	if maskLen != 32 {
		// IPv4 should should not hit this.
		return nil, fmt.Errorf("bug: virtual ip mask is of %v bits length", maskLen)
	}
	msgOverlay.VirtualMask = uint32(maskOnes)

	if msgPeer.Details, err = ptypes.MarshalAny(msgOverlay); err != nil {
		return nil, err
	}
	return
}

func (p *L3Peer) applyPBSnapshot(tx *L3PeerReleaseTx, msg *pbp.Peer) (err error) {
	p.PeerMeta.applyPBSnapshot(tx.PeerReleaseTx, msg)
	if !tx.IsNewVersion() {
		return nil
	}

	overlayMsg := &pbp.OverlayPeer{}
	if err = ptypes.UnmarshalAny(msg.Details, overlayMsg); err != nil {
		return err
	}
	// overlay network
	ip := net.IP(make([]byte, 4))
	binary.BigEndian.PutUint32(ip, overlayMsg.VirtualIp)
	tx.Net(ip, net.CIDRMask(int(overlayMsg.VirtualMask), 32))
	// is router ?
	tx.IsRouter(overlayMsg.IsRouter)
	return nil
}

func (p *L3Peer) ApplyPBSnapshot(msg *pbp.Peer) (err error) {
	if msg == nil {
		return nil
	}
	p.Tx(func(bp Peer, tx *L3PeerReleaseTx) bool {
		if err = p.applyPBSnapshot(tx, msg); err != nil {
			return false
		}
		return tx.IsNewVersion()
	})
	return
}

func (p *L3Peer) Tx(commit func(Peer, *L3PeerReleaseTx) bool) (commited bool) {
	parentCommited := p.PeerMeta.Tx(func(bp Peer, btx *PeerReleaseTx) bool {
		tx, shouldCommit := &L3PeerReleaseTx{
			p:             p,
			PeerReleaseTx: btx,
		}, false

		shouldCommit = commit(p, tx)
		if !shouldCommit {
			return false
		}
		commited = true
		tx.Version(p.generateVersion())

		if tx.isRouterUpdated {
			p.isRouter = tx.isRouter
		}
		if tx.ip != nil {
			p.ip = tx.ip
		}
		if tx.mask != nil {
			p.mask = tx.mask
		}
		return true
	})

	return parentCommited || commited
}
