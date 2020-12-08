package edgerouter

import (
	"container/heap"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/edgerouter/gossip"
	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/sladder"
)

type rawUnderlayInfo struct {
	ID  int32
	IPs common.IPNetSet
}

type pendingOverlayMetadataUpdation struct {
	p         *metanet.MetaPeer
	notBefore time.Time

	pendingSet uint32
	missingSet uint32

	nets     map[driver.NetworkID]*gossip.OverlayNetworkV1
	underlay *rawUnderlayInfo
}

func (u *pendingOverlayMetadataUpdation) Less(x *pendingOverlayMetadataUpdation) bool {
	if u == nil {
		return false
	}
	if x == nil {
		return true
	}
	if u.notBefore.IsZero() {
		return true
	}
	return u.notBefore.Before(x.notBefore)
}

const (
	pendingNetworkUpdation      = uint32(0x1)
	pendingUnderlayInfoUpdation = uint32(0x2)
)

func (u *pendingOverlayMetadataUpdation) IsPending(pendingSet uint32) bool {
	return pendingSet&u.pendingSet != 0
}
func (u *pendingOverlayMetadataUpdation) SetPending(pendingSet uint32)   { u.pendingSet |= pendingSet }
func (u *pendingOverlayMetadataUpdation) ResetPending(pendingSet uint32) { u.pendingSet &^= pendingSet }

func (u *pendingOverlayMetadataUpdation) IsMissing(set uint32) bool {
	return u.missingSet&u.pendingSet&set != 0
}
func (u *pendingOverlayMetadataUpdation) ResetMissing(set uint32) { u.missingSet &^= set }
func (u *pendingOverlayMetadataUpdation) SetMissing(set uint32)   { u.missingSet |= set }

func (u *pendingOverlayMetadataUpdation) ResetNetworksPending() {
	u.ResetPending(pendingNetworkUpdation)
}

func (u *pendingOverlayMetadataUpdation) PendingNetworks(new map[driver.NetworkID]*gossip.OverlayNetworkV1) {
	u.SetPending(pendingNetworkUpdation)
	u.nets = new
	if u.nets == nil {
		u.SetMissing(pendingNetworkUpdation)
	} else {
		u.ResetMissing(pendingNetworkUpdation)
	}
}

func (u *pendingOverlayMetadataUpdation) MergePendingNetworks(x *pendingOverlayMetadataUpdation) {
	u.nets = x.nets
}

func (u *pendingOverlayMetadataUpdation) IsNetworksPending() bool {
	return u.IsPending(pendingNetworkUpdation)
}

func (u *pendingOverlayMetadataUpdation) IsNetworksMissing() bool {
	return u.IsMissing(pendingNetworkUpdation)
}

func (u *pendingOverlayMetadataUpdation) Networks() map[driver.NetworkID]*gossip.OverlayNetworkV1 {
	return u.nets
}

func (u *pendingOverlayMetadataUpdation) PendingUnderlayInfo(new *rawUnderlayInfo) {
	u.SetPending(pendingUnderlayInfoUpdation)
	u.underlay = new
	if u.underlay == nil {
		u.SetMissing(pendingUnderlayInfoUpdation)
	} else {
		u.ResetMissing(pendingUnderlayInfoUpdation)
	}
}

func (u *pendingOverlayMetadataUpdation) MergePendingUnderlayInfo(x *pendingOverlayMetadataUpdation) {
	u.underlay = x.underlay
}

func (u *pendingOverlayMetadataUpdation) IsUnderlayInfoPending() bool {
	return u.IsPending(pendingUnderlayInfoUpdation)
}

func (u *pendingOverlayMetadataUpdation) IsUnderlayInfoMissing() bool {
	return u.IsMissing(pendingUnderlayInfoUpdation)
}

func (u *pendingOverlayMetadataUpdation) UnderlayInfo() *rawUnderlayInfo { return u.underlay }

func (u *pendingOverlayMetadataUpdation) Merge(x *pendingOverlayMetadataUpdation) {
	u.MergePendingNetworks(x)
	u.MergePendingUnderlayInfo(x)
}

type pendingOverlayMetadataUpdationQueue []*pendingOverlayMetadataUpdation

func (q pendingOverlayMetadataUpdationQueue) Less(i, j int) bool { return q[i].Less(q[j]) }
func (q pendingOverlayMetadataUpdationQueue) Len() int           { return len(q) }
func (q pendingOverlayMetadataUpdationQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *pendingOverlayMetadataUpdationQueue) Push(x interface{}) {
	*q = append(*q, x.(*pendingOverlayMetadataUpdation))
}

func (q *pendingOverlayMetadataUpdationQueue) Pop() interface{} {
	o := *q
	idx := o.Len() - 1
	old := o[idx]
	*q = o[:idx]
	return old
}

type networkInfo struct {
	r          *EdgeRouter
	ID         int32
	driverType driver.OverlayDriverType

	lock sync.RWMutex

	driverCtx *driverContext

	peers               map[*metanet.MetaPeer]struct{}
	latestRemoteOptions map[*metanet.MetaPeer]map[string][]byte
	underlayIDWatcher   map[*driver.UnderlayIDWatcher]struct{}
	ipWatcher           map[*driver.UnderlayIPWatcher]struct{}
}

func newNetworkInfo(r *EdgeRouter, id int32, driverType driver.OverlayDriverType) *networkInfo {
	return &networkInfo{
		r:                   r,
		underlayIDWatcher:   make(map[*driver.UnderlayIDWatcher]struct{}),
		ipWatcher:           make(map[*driver.UnderlayIPWatcher]struct{}),
		peers:               make(map[*metanet.MetaPeer]struct{}),
		latestRemoteOptions: make(map[*metanet.MetaPeer]map[string][]byte),
		ID:                  id,
	}
}

func (i *networkInfo) detachNotifyDriverContext() {
	drvCtx := i.driverCtx
	if drvCtx == nil {
		return
	}

	for p := range i.peers {
		drvCtx.PeerLeft(i, p)
	}
	i.driverCtx = nil
}

func (i *networkInfo) attachNotifyDriverContext() {
	drvCtx := i.driverCtx
	if drvCtx == nil {
		return
	}

	// attach.
	for p := range i.peers {
		drvCtx.PeerJoin(i, p)
		if opt, _ := i.latestRemoteOptions[p]; opt != nil {
			drvCtx.ProcessNewRemoteOptions(i.ID, p, opt)
		}
	}
}

func (i *networkInfo) IsIdle() bool  { return i.driverCtx == nil }
func (i *networkInfo) IsStray() bool { return len(i.peers) < 1 }

func (i *networkInfo) Peers() (peers []*metanet.MetaPeer) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	for peer := range i.peers {
		peers = append(peers, peer)
	}
	return
}

func (i *networkInfo) UnderlayID(p *metanet.MetaPeer) int32 { return i.r.PeerUnderlayID(p) }
func (i *networkInfo) PeerIPs(p *metanet.MetaPeer) (public, private common.IPNetSet) {
	return i.r.PeerIPs(p)
}

func (i *networkInfo) WatchUnderlayID(handler driver.UnderlayIDWatcher) {
	if handler == nil {
		return
	}

	i.lock.Lock()
	defer i.lock.Unlock()

	i.underlayIDWatcher[&handler] = struct{}{}
}

func (i *networkInfo) WatchPeerIPs(handler driver.UnderlayIPWatcher) {
	if handler == nil {
		return
	}

	i.lock.Lock()
	defer i.lock.Unlock()

	i.ipWatcher[&handler] = struct{}{}
}

func (i *networkInfo) ensurePeerPresent(p *metanet.MetaPeer, present bool) {
	// update peer set.
	_, hasPeer := i.peers[p]
	if hasPeer == present {
		return
	}
	if !present {
		delete(i.peers, p)
		delete(i.latestRemoteOptions, p)
	} else {
		i.peers[p] = struct{}{}
	}

	// delegate to driver context.
	drvCtx := i.driverCtx
	if i.driverCtx == nil {
		// network is idle. skip.
		return
	}
	if !present {
		drvCtx.PeerLeft(i, p)
	} else {
		drvCtx.PeerJoin(i, p)
	}
}

func (i *networkInfo) processNewRemoteOptions(netID int32, p *metanet.MetaPeer, opts map[string][]byte) {
	if _, hasPeer := i.peers[p]; !hasPeer {
		return
	}

	cached := make(map[string][]byte, len(opts))
	for k, v := range opts {
		cached[k] = v
	}
	i.latestRemoteOptions[p] = cached

	// delegate to driver context.
	drvCtx := i.driverCtx
	if drvCtx == nil {
		return
	}
	drvCtx.ProcessNewRemoteOptions(netID, p, opts)
}

func (i *networkInfo) processNewUnderlayInfo(p *metanet.MetaPeer, id int32, ips common.IPNetSet) {
	// notify.
	for watcher := range i.underlayIDWatcher {
		if !(*watcher)(p, id) {
			delete(i.underlayIDWatcher, watcher)
		}
	}

	if len(i.ipWatcher) > 0 {
		var private, public common.IPNetSet
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() {
				continue
			}
			for _, privateNet := range common.PrivateUnicastIPNets {
				if privateNet.Contains(ip.IP) {
					private = append(private, ip)
				} else {
					public = append(public, ip)
				}
			}
		}

		for watcher := range i.ipWatcher {
			if !(*watcher)(p, public, private) {
				delete(i.ipWatcher, watcher)
			}
		}
	}
}

func (r *EdgeRouter) initializeNetworkMap() (err error) {
	// data models.
	r.overlayModel = &gossip.OverlayNetworksValidatorV1{}
	r.overlayModelKey = gossip.DefaultOverlayNetworkKey
	if err = r.metaNet.RegisterDataModel(r.overlayModelKey, r.overlayModel, true, false, 0); err != nil {
		return err
	}

	// watch for peer.
	r.metaNet.RegisterPeerJoinWatcher(r.onPeerJoin)
	r.metaNet.RegisterPeerLeaveWatcher(r.onPeerLeft)
	// watch for overlay metadata changes.
	if !r.metaNet.WatchKeyChanges(r.onOverlayNetworkStateChanged, gossip.DefaultOverlayNetworkKey) {
		return errors.New("cannot watch overlay metadata changes")
	}

	r.republishC <- republishAll

	return nil
}

func (r *EdgeRouter) onPeerJoin(peer *metanet.MetaPeer) bool {
	updation := &pendingOverlayMetadataUpdation{
		p: peer,
	}
	updation.PendingNetworks(nil)
	updation.PendingUnderlayInfo(nil)

	r.pendingAppendC <- updation

	return true
}

func (r *EdgeRouter) onPeerLeft(peer *metanet.MetaPeer) bool {
	r.pendingAppendC <- &pendingOverlayMetadataUpdation{
		p: peer,
	}

	return true
}

func (r *EdgeRouter) onOverlayNetworkStateChanged(peer *metanet.MetaPeer, meta sladder.KeyValueEventMetadata) bool {
	switch meta.Event() {
	case sladder.KeyInsert:
		meta := meta.(sladder.KeyInsertEventMetadata)
		r.submitMetadataUpdationFromRaw(peer, meta.Value())
	case sladder.ValueChanged:
		meta := meta.(sladder.KeyChangeEventMetadata)
		r.submitMetadataUpdationFromRaw(peer, meta.New())
	case sladder.KeyDelete:
		r.pendingAppendC <- &pendingOverlayMetadataUpdation{
			p: peer,
		}
	}
	return true
}

func (r *EdgeRouter) getRepublishInverval() time.Duration {
	d := r.RepublishInterval
	if d < 1 {
		d = defaultRepublishInterval
	}
	return d
}

func (r *EdgeRouter) startCollectUnderlayAutoIPs() {
	r.arbiters.main.Go(func() {
	refreshAutoIPs:
		for r.arbiters.main.ShouldRun() {
			select {
			case <-r.arbiters.driver.Exit():
				break refreshAutoIPs
			case <-time.After(defaultAutoIPRefreshInterval):
			}

			var newAutoIPs common.IPNetSet = nil

			if err := <-r.submitVirtualDo(driver.NetIDUnderlay, false, func(ctx *virtualNetworkDoContext) error {
				ifaces, err := net.Interfaces()
				if err != nil {
					r.log.Error("failed to enumerate network interfaces. (err = \"%v\")", err)
					return nil
				}

				for _, iface := range ifaces {
					addrs, ierr := iface.Addrs()
					if ierr != nil {
						r.log.Error("failed to get ip address for interface %v. (err = \"%v\")", err)
						continue
					}

					for _, addr := range addrs {
						ip, isIPNet := addr.(*net.IPNet)
						if !isIPNet { // parse addr.String() for safety.
							unparsed := addr.String()
							if _, ip, err = net.ParseCIDR(unparsed); err != nil {
								// ignore address in invalid notation.
								r.log.Warn("parse failed. invalid interface ip address \"%v\". (err = \"%v\")", err)
								continue
							}
						}

						newAutoIPs = append(newAutoIPs, ip)
					}
				}

				return nil
			}); err != nil {
				r.log.Error("virtual do fails when collecting underlay auto IPs. (err = \"%v\")", err)
				continue
			}

			newAutoIPs.Build()

			r.lock.Lock()
			newAutoIPs.Remove(r.underlay.staticIPs...)
			if !r.underlay.autoIPs.Equal(&newAutoIPs) {
				r.underlay.autoIPs = newAutoIPs
				r.republishC <- republishAll // republish.
			}
			r.lock.Unlock()
		}
	})
}

func (r *EdgeRouter) startRepublishLocalStates() {
	r.arbiters.main.Go(func() {
		active := uint32(0)
		reportC := make(chan uint32)

	publishProc:
		for r.arbiters.main.ShouldRun() {
			for active < 1 { // idle.
				select {
				case <-r.arbiters.main.Exit():
					break publishProc
				case reqs := <-r.republishC:
					active |= reqs
				}
			}

			// changes pending.
			afterC := time.After(r.getRepublishInverval())
			for {
				select {
				case <-afterC:
				case <-r.arbiters.main.Exit():
					break publishProc
				case reqs := <-r.republishC:
					active |= reqs
					continue
				}
				break
			}

			// publish.
			activeSet := active
			r.arbiters.main.Go(func() {
				r.lock.Lock()
				defer r.lock.Unlock()
				reportC <- r.republishOverlayNetworks(activeSet)
			})
			active = 0
			for {
				select {
				case reqs := <-r.republishC:
					active |= reqs
					continue

				case reqs := <-reportC:
					active |= reqs
				}
				break
			}
		}
	})
}

// delayRepublishOverlayNetworks is responsible for publishing local states.
func (r *EdgeRouter) republishOverlayNetworks(activeSet uint32) uint32 {
	// TODO(xutao): using flags as hint to avoid unnecessary publish trys.

	var errs common.Errors

	errs.Trace(r.metaNet.SladderTxn(func(t *sladder.Transaction) bool {
		rtx, terr := t.KV(t.Cluster.Self(), r.overlayModelKey)
		if terr != nil {
			errs.Trace(terr)
			r.log.Errorf("cannot get overlay gossip metadata in transaction. (err = \"%v\")", terr)
			return false
		}
		tx := rtx.(*gossip.OverlayNetworksV1Txn)

		// publish underlay info.
		{
			tx.SetUnderlayID(r.underlay.ID)
			ips := r.underlay.autoIPs.Clone()
			ips = append(ips, r.underlay.staticIPs.Clone()...)
			tx.SetUnderlayIPs(ips)
		}

		// publish networks.
		oldNetIDMap := map[driver.NetworkID]struct{}{}
		for _, netID := range tx.NetworkList() {
			oldNetIDMap[netID] = struct{}{}
		}
		// publish removed networks.
		for netID := range oldNetIDMap {
			info, _ := r.networks[netID.ID]
			if info == nil || // removed network.
				info.driverType != netID.DriverType || // replaced driver.
				info.driverCtx == nil { // removed driver.
				tx.RemoveNetwork(netID)
				continue
			}
		}
		// publish new networks.
		for id, info := range r.networks {
			if info == nil {
				r.log.Warn("[BUGS!] got nil networkInfo in network set.")
				continue
			}
			if info.driverCtx == nil { // unavaliable driver.
				continue
			}
			netID := driver.NetworkID{
				ID: id, DriverType: info.driverType,
			}
			if _, published := oldNetIDMap[netID]; published {
				continue
			}
			if terr = tx.AddNetwork(netID); terr != nil {
				errs.Trace(terr)
				r.log.Errorf("failed to add new network in transaction. (err = \"%v\")", terr)
				return false
			}
		}

		// publish dymanic options.
		for id, info := range r.networks {
			if info.driverCtx == nil { // unavaliable driver.
				continue
			}
			drv := info.driverCtx
			netID := driver.NetworkID{
				ID: id, DriverType: info.driverType,
			}

			drv.lock.RLock()
			netMap, _ := drv.networkMap[id]
			if netMap == nil {
				drv.lock.RUnlock()
				continue
			}
			netMap.lock.RLock()

			paramV1 := tx.NetworkFromID(netID)
			paramV1.Params = make(map[string][]byte, len(netMap.localOptions))
			for k, v := range netMap.localOptions {
				paramV1.Params[k] = v
			}

			netMap.lock.RUnlock()
			drv.lock.RUnlock()
		}

		return true
	}))

	if errs != nil { // retry.
		return republishAll
	}

	return republishNone
}

func (r *EdgeRouter) applyRemotePeerStates(updation *pendingOverlayMetadataUpdation) {
	if updation.IsNetworksPending() {
		r.updateOverlayNetworkMetadata(updation.p, updation.Networks())
	}
	if updation.IsUnderlayInfoPending() {
		r.updateUnderlayInfomation(updation.p, updation.UnderlayInfo().ID, updation.UnderlayInfo().IPs)
	}
}

func (r *EdgeRouter) applyPeerLeftStates(p *metanet.MetaPeer) {
	delete(r.underlay.ids, p)
	delete(r.underlay.ips, p)

	for _, info := range r.networks {
		info.ensurePeerPresent(p, false)
	}
	for netID, info := range r.idleNetworks {
		info.ensurePeerPresent(p, false)
		if info.IsStray() {
			delete(r.idleNetworks, netID)
		}
	}
}

func (r *EdgeRouter) completePendingUpdation(updation *pendingOverlayMetadataUpdation) error {
	var errs common.Errors

	autoCompletePendings := pendingNetworkUpdation | pendingUnderlayInfoUpdation

	if updation.IsMissing(autoCompletePendings) {
		// complete missing metadata.
		errs.Trace(r.metaNet.SladderTxn(func(t *sladder.Transaction) bool {
			if !t.KeyExists(updation.p.SladderNode(), r.overlayModelKey) {
				return false
			}

			rtx, err := t.KV(updation.p.SladderNode(), r.overlayModelKey)
			if err != nil {
				errs.Trace(err)
				r.log.Errorf("cannot open overlay network metadata. (err = \"%v\")", err)
				return false
			}
			tx := rtx.(*gossip.OverlayNetworksV1Txn)

			// complete raw overlay metadata.
			if updation.IsNetworksMissing() {
				netList := tx.NetworkList()
				nets := make(map[driver.NetworkID]*gossip.OverlayNetworkV1, len(netList))
				for _, netID := range netList {
					net := tx.NetworkFromID(netID)
					if net == nil {
						r.log.Warn("[BUG!] got nil OverlayNetworkV1 whilt netID is valid.")
						continue
					}
					nets[netID] = net
				}
				updation.PendingNetworks(nets)
			}

			// complete underlay info.
			if updation.IsUnderlayInfoMissing() {
				updation.PendingUnderlayInfo(&rawUnderlayInfo{
					ID:  tx.UnderlayID(),
					IPs: tx.UnderlayIPs(),
				})
			}

			return false
		}))
		if err := errs.AsError(); err != nil {
			return err
		}
	}

	return nil
}

func (r *EdgeRouter) startProcessPendingUpdates(processC chan *pendingOverlayMetadataUpdation) {
	r.arbiters.main.Go(func() {
		var updation *pendingOverlayMetadataUpdation

	processPending:
		for r.arbiters.main.ShouldRun() {
			select {
			case <-r.arbiters.main.Exit():
				break processPending
			case updation = <-processC:
			}

			if updation.p.IsLeft() { // left peer.
				r.lock.Lock()
				r.applyPeerLeftStates(updation.p)
				r.lock.Unlock()

				continue
			}

			if err := r.completePendingUpdation(updation); err != nil {
				r.log.Error("cannot complete network raw metadata for peer. failed to apply remote state. [peer = %v] (err = \"%v\")", updation.p, err)
				updation.notBefore = time.Now().Add(time.Second * 5) // delay retry.
				r.pendingAppendC <- updation
				continue
			}

			r.lock.Lock()
			r.applyRemotePeerStates(updation)
			r.lock.Unlock()
		}
	})
}

func (r *EdgeRouter) startAcceptPendingUpdates(appendC, processC chan *pendingOverlayMetadataUpdation) {
	r.arbiters.main.Go(func() {
		pendingUpdation := make(map[*metanet.MetaPeer]*pendingOverlayMetadataUpdation)
		pendingQueue := make(pendingOverlayMetadataUpdationQueue, 0, 2)
		heap.Init(&pendingQueue)

		enqueuePending := func(updation *pendingOverlayMetadataUpdation) {
			if updation == nil || updation.p == nil {
				return
			}
			recentPending, _ := pendingUpdation[updation.p]
			if recentPending != nil {
				recentPending.Merge(updation)
			} else {
				pendingUpdation[updation.p] = updation
				heap.Push(&pendingQueue, updation)
			}
		}

		dequeuePending := func() (next *pendingOverlayMetadataUpdation) {
			if len(pendingQueue) < 1 {
				return nil
			}
			return heap.Pop(&pendingQueue).(*pendingOverlayMetadataUpdation)
		}

	acceptPending:
		for r.arbiters.main.ShouldRun() {
			for len(pendingUpdation) < 1 { // idle.
				select {
				case <-r.arbiters.main.Exit():
					break acceptPending
				case updation := <-appendC:
					enqueuePending(updation)
				}
			}

			// feed.
			var afterC <-chan time.Time
			nextUpdation := dequeuePending()
			if !nextUpdation.notBefore.IsZero() {
				now := time.Now()
				after := nextUpdation.notBefore.Sub(now)
				if after > 0 {
					afterC = time.After(after)
				}
			}
			for nextUpdation != nil {
				if afterC != nil {
					select {
					case <-r.arbiters.main.Exit():
						break acceptPending

					case updation := <-appendC:
						enqueuePending(updation)
						if updation.Less(nextUpdation) {
							enqueuePending(nextUpdation)
							nextUpdation = nil
						}
					case <-afterC:
						afterC = nil
					}

				} else {
					select {
					case <-r.arbiters.main.Exit():
						break acceptPending

					case updation := <-appendC:
						enqueuePending(updation)
						if updation.Less(nextUpdation) {
							enqueuePending(nextUpdation)
							nextUpdation = nil
						}
					case processC <- nextUpdation:
						nextUpdation = nil
					}
				}
			}
		}
	})
}

func (r *EdgeRouter) submitMetadataUpdationFromRaw(peer *metanet.MetaPeer, raw string) {
	nets := &gossip.OverlayNetworksV1{}
	updation := &pendingOverlayMetadataUpdation{p: peer}
	if err := nets.DecodeString(raw); err != nil {
		r.log.Warn("got invalid raw OverlayNetworksV1 stream.")

		updation.PendingNetworks(nil)
		updation.PendingUnderlayInfo(nil)
	} else {
		updation.PendingNetworks(nets.Networks)
		updation.PendingUnderlayInfo(&rawUnderlayInfo{
			ID:  nets.UnderlayID,
			IPs: nets.UnderlayIPs,
		})
	}

	r.pendingAppendC <- updation
}

func (r *EdgeRouter) updateOverlayNetworkMetadata(peer *metanet.MetaPeer, nets map[driver.NetworkID]*gossip.OverlayNetworkV1) {
	// detect peer joining and remote options changes.
	for netID, net := range nets {
		info, _ := r.networks[netID.ID]
		if info == nil || info.driverType != netID.DriverType { // idle networks.
			idleInfo, hasIdleInfo := r.idleNetworks[netID]
			if idleInfo == nil {
				hasIdleInfo = false
			}
			if !hasIdleInfo {
				idleInfo = newNetworkInfo(r, netID.ID, netID.DriverType)
			}

			// may modify peer set, lock first.
			idleInfo.lock.Lock()
			idleInfo.ensurePeerPresent(peer, true)
			idleInfo.processNewRemoteOptions(netID.ID, peer, net.Params) // may not needed?
			idleInfo.lock.Unlock()

			r.idleNetworks[netID] = idleInfo

		} else { // active network.
			// may modify peer set, lock first.
			info.lock.Lock()
			info.ensurePeerPresent(peer, true)
			info.processNewRemoteOptions(netID.ID, peer, net.Params)
			info.lock.Unlock()
		}
	}

	// detect peer left.
	for overlayID, info := range r.networks {
		info.lock.Lock()
		netID := driver.NetworkID{
			ID: overlayID, DriverType: info.driverType,
		}
		if _, isNetMember := nets[netID]; !isNetMember {
			info.ensurePeerPresent(peer, false)
		}
		info.lock.Unlock()
	}
	for netID, info := range r.idleNetworks {
		info.lock.Lock()
		if _, isNetMember := nets[netID]; !isNetMember {
			info.ensurePeerPresent(peer, false)
		}
		if len(info.peers) < 1 {
			delete(r.idleNetworks, netID)
		}
		info.lock.Unlock()
	}
}

func (r *EdgeRouter) updateUnderlayInfomation(peer *metanet.MetaPeer, newID int32, newIPs common.IPNetSet) {
	changed := false

	oldID, hasOldID := r.underlay.ids[peer]
	if !hasOldID {
		newID = -1
		changed = true
		r.underlay.ids[peer] = newID

	} else if oldID != newID {
		changed = true
		r.underlay.ids[peer] = newID
	}

	if newIPs != nil {
		oldIPs, hasOldIPs := r.underlay.ips[peer]
		if !hasOldIPs || !oldIPs.Equal(&newIPs) {
			r.underlay.ips[peer] = newIPs
			changed = true
		} else {
			newIPs = newIPs[:0]
			newIPs.Merge(oldIPs)
		}
	}

	if !changed {
		return
	}

	for _, info := range r.networks {
		info.lock.Lock()
		info.processNewUnderlayInfo(peer, newID, newIPs)
		info.lock.Unlock()
	}
	for _, info := range r.idleNetworks {
		info.lock.Lock()
		info.processNewUnderlayInfo(peer, newID, newIPs)
		info.lock.Unlock()
	}
}
