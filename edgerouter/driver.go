package edgerouter

import (
	"io"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/metanet"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

type driverMessager struct {
	driverType driver.OverlayDriverType
	r          *EdgeRouter
}

func (m *driverMessager) Send(peer *metanet.MetaPeer, payload []byte) {
	m.r.fastSendVNetDriverMessage(peer, m.driverType, func(w io.Writer) {
		// TODO(xutao): avoid data copy.
		w.Write(payload)
	})
}

func (m *driverMessager) WatchMessage(handler driver.MessageHandler) driver.MessageHandler {
	return m.r.watchForDriverMessage(m.driverType, handler)
}

type driverNetworkMap struct {
	lock sync.RWMutex

	info *networkInfo

	membershipChangesWatcher map[*driver.PeerEventHandler]struct{}

	localOptions        map[string][]byte
	remoteOptions       map[*metanet.MetaPeer]map[string][]byte
	remoteOptionWatcher map[*driver.RemoteOptionMapWatcher]struct{}

	overlayIPs       map[*metanet.MetaPeer]common.IPNetSet
	overlayIPWatcher map[*driver.IPWatcher]struct{}
}

func newDriverNerworkMap(info *networkInfo) *driverNetworkMap {
	return &driverNetworkMap{
		info:                     info,
		membershipChangesWatcher: make(map[*driver.PeerEventHandler]struct{}),
		localOptions:             map[string][]byte{},
		remoteOptions:            make(map[*metanet.MetaPeer]map[string][]byte),
		remoteOptionWatcher:      make(map[*driver.RemoteOptionMapWatcher]struct{}),

		overlayIPs:       make(map[*metanet.MetaPeer]common.IPNetSet),
		overlayIPWatcher: make(map[*driver.IPWatcher]struct{}),
	}
}

func (m *driverNetworkMap) WatchOverlayIPs(watcher driver.IPWatcher) {
	if watcher == nil {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	m.overlayIPWatcher[&watcher] = struct{}{}
}

func (m *driverNetworkMap) OverlayIPs(p *metanet.MetaPeer) common.IPNetSet {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, _ := m.overlayIPs[p]
	return set
}

func (m *driverNetworkMap) WatchMemebershipChanged(handler driver.PeerEventHandler) {
	if handler == nil {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	m.membershipChangesWatcher[&handler] = struct{}{}
}

func (m *driverNetworkMap) GetLocalOption(key string) []byte {
	m.lock.RLock()
	defer m.lock.RUnlock()

	data, _ := m.localOptions[key]
	return data
}

func (m *driverNetworkMap) SetLocalOption(key string, data []byte) []byte {
	m.lock.Lock()
	defer m.lock.Unlock()

	old, _ := m.localOptions[key]

	m.localOptions[key] = data

	// TODO(xutao): publish new data.

	return old
}

func (m *driverNetworkMap) Peers() []*metanet.MetaPeer { return m.info.Peers() }

func (m *driverNetworkMap) RemoteOptions(p *metanet.MetaPeer) map[string][]byte {
	m.lock.RLock()
	defer m.lock.RUnlock()

	opts, _ := m.remoteOptions[p]
	if opts == nil {
		return nil
	}
	cloned := make(map[string][]byte, len(opts))
	for k, v := range opts {
		clonedv := make([]byte, 0, len(v))
		clonedv = append(clonedv, v...)
		cloned[k] = v
	}
	return cloned
}

func (m *driverNetworkMap) WatchRemoteOptions(handler driver.RemoteOptionMapWatcher) {
	if handler == nil {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	m.remoteOptionWatcher[&handler] = struct{}{}
}

func (m *driverNetworkMap) PeerJoin(p *metanet.MetaPeer) {
	m.lock.Lock()

	_, hasPeer := m.remoteOptions[p]
	if hasPeer {
		return
	}
	m.remoteOptions[p] = nil

	m.lock.Unlock()

	// notify.
	// TODO(xutao): ensure order of notification when PeerJoin() is
	//		called by multiple goroutines, although only single goroutine will call this so far.
	for handler := range m.membershipChangesWatcher {
		if !(*handler)(p, driver.PeerLeft) { // canceled?
			delete(m.membershipChangesWatcher, handler)
		}
	}
}

func (m *driverNetworkMap) PeerLeft(p *metanet.MetaPeer) {
	m.lock.Lock()

	_, hasPeer := m.remoteOptions[p]
	if !hasPeer {
		return
	}

	delete(m.remoteOptions, p)
	delete(m.overlayIPs, p)

	m.lock.Unlock()

	// TODO(xutao): ensure order of notification when PeerLeft() is
	//		called by multiple goroutines, although only single goroutine will call this so far.
	for handler := range m.membershipChangesWatcher {
		if !(*handler)(p, driver.PeerLeft) { // canceled?
			delete(m.membershipChangesWatcher, handler)
		}
	}
}

func (m *driverNetworkMap) ProcessOverlayIPs(p *metanet.MetaPeer, ips common.IPNetSet) {
	if p == nil {
		return
	}

	m.lock.Lock()

	prevIPs, _ := m.overlayIPs[p]
	if prevIPs.Equal(&ips) {
		return
	}

	m.overlayIPs[p] = ips.Clone()

	m.lock.Unlock()

	// TODO(xutao): ensure order of notification when ProcessOverlayIPs() is
	//		called by multiple goroutines, although only single goroutine will call this so far.
	for watcher := range m.overlayIPWatcher {
		if !(*watcher)(p, ips.Clone()) {
			delete(m.overlayIPWatcher, watcher)
		}
	}
}

func (m *driverNetworkMap) ProcessNewRemoteOptions(p *metanet.MetaPeer, opts map[string][]byte) {
	if p == nil {
		return
	}

	m.lock.Lock()

	if _, hasPeer := m.remoteOptions[p]; !hasPeer {
		return // ignore remote options for absent peer.
	}

	newOpts := make(map[string][]byte, len(opts))
	for k, v := range opts {
		newOpts[k] = v
	}
	m.remoteOptions[p] = newOpts

	m.lock.Unlock()

	// TODO(xutao): ensure order of notification when ProcessNewRemoteOptions() is
	//		called by multiple goroutines, although only single goroutine will call this so far.
	for handler := range m.remoteOptionWatcher {
		if !(*handler)(p, newOpts) {
			delete(m.remoteOptionWatcher, handler)
		}
	}
}

type driverContext struct {
	router *EdgeRouter
	store  common.SubpathStore
	logger *logging.Entry

	lock sync.RWMutex

	driverType driver.OverlayDriverType
	driver     driver.OverlayDriver
	messager   *driverMessager

	networkMap        map[int32]*driverNetworkMap
	underlayIDWatcher map[*driver.UnderlayIDWatcher]struct{}
	ipWatcher         map[*driver.UnderlayIPWatcher]struct{}
}

func newEmptyDriverContext(router *EdgeRouter) *driverContext {
	d := &driverContext{
		router:            router,
		networkMap:        make(map[int32]*driverNetworkMap),
		underlayIDWatcher: make(map[*driver.UnderlayIDWatcher]struct{}),
	}
	return d
}

func (c *driverContext) Store() common.Store       { return &c.store }
func (c *driverContext) Arbiter() *arbit.Arbiter   { return c.router.arbiters.driver }
func (c *driverContext) Messager() driver.Messager { return c.messager }
func (c *driverContext) NetworkMap(netID int32) driver.OverlayNetworkMap {
	c.lock.RLock()
	defer c.lock.RUnlock()

	netMap, _ := c.networkMap[netID]
	return netMap
}

func (c *driverContext) Logger() driver.Logger { return c.logger }

func (c *driverContext) VirtualDo(netID int32, process func(underlayID, overlayID int32) error) error {
	return c.router.VirtualDo(netID, process)
}

func (c *driverContext) UnderlayID(p *metanet.MetaPeer) int32 {
	return c.router.PeerUnderlayID(p)
}

func (c *driverContext) WatchUnderlayID(watcher driver.UnderlayIDWatcher) {
	if watcher == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	c.underlayIDWatcher[&watcher] = struct{}{}
}

func (c *driverContext) PeerIPs(p *metanet.MetaPeer) (public, private common.IPNetSet) {
	return c.router.PeerIPs(p)
}

func (c *driverContext) WatchPeerIPs(watcher driver.UnderlayIPWatcher) {
	if watcher == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	c.ipWatcher[&watcher] = struct{}{}
}

func (c *driverContext) PeerJoin(info *networkInfo, p *metanet.MetaPeer) {
	if info == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	netMap, _ := c.networkMap[info.ID]
	if netMap == nil {
		netMap = newDriverNerworkMap(info)
	}
	netMap.PeerJoin(p)
}

func (c *driverContext) PeerLeft(info *networkInfo, p *metanet.MetaPeer) {
	if info == nil {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	netMap, _ := c.networkMap[info.ID]
	if netMap == nil {
		return
	}
	netMap.PeerLeft(p)
}

func (c *driverContext) ProcessNewRemoteOptions(netID int32, p *metanet.MetaPeer, opts map[string][]byte) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	netMap, _ := c.networkMap[netID]
	if netMap == nil {
		return
	}
	netMap.ProcessNewRemoteOptions(p, opts)
}

func (c *driverContext) ProcessOverlayIPs(netID int32, p *metanet.MetaPeer, ips common.IPNetSet) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	netMap, _ := c.networkMap[netID]
	if netMap == nil {
		return
	}
	netMap.ProcessOverlayIPs(p, ips)
}

func (c *driverContext) ProcessNewUnderlayInfo(p *metanet.MetaPeer, id int32, ips common.IPNetSet) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	// notify.
	for watcher := range c.underlayIDWatcher {
		if !(*watcher)(p, id) {
			delete(c.underlayIDWatcher, watcher)
		}
	}

	if len(c.ipWatcher) > 0 {
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

		for watcher := range c.ipWatcher {
			if !(*watcher)(p, public, private) {
				delete(c.ipWatcher, watcher)
			}
		}
	}
}

const (
	rootOverlayNamespaceLinkName = "eth0"
)

func (e *EdgeRouter) delayActivateDriver(driverType driver.OverlayDriverType, driver driver.OverlayDriver, d time.Duration) {
	e.arbiters.main.Go(func() {
		if d > 0 {
			select {
			case <-time.After(d):
			case <-e.arbiters.main.Exit():
				return
			}
		}

		e.lock.Lock()
		defer e.lock.Unlock()

		needRetry := false

		drvCtx, _ := e.drivers[driverType]
		if drvCtx.driver != driver { // driver changed?
			return
		}

		changed := false

		// attach to existing networks.
		for netID, info := range e.networks {
			if info.driverType != driverType {
				continue
			}

			if err := e.ensureNetworkNamespace(netID); err != nil {
				e.log.Warnf("failed to ensure namespace for net %v. [driverType = %v] (err = \"%v\")", netID, driverType)
				needRetry = true
				continue
			}

			info.lock.Lock()

			oldCtx := info.driverCtx
			if oldCtx != nil {
				if err := oldCtx.driver.DelLink(rootOverlayNamespaceLinkName, netID); err != nil {
					e.log.Warnf("failed to remove link from net %v. [driverType = %v] (err = \"%v\")", netID, driverType)
					needRetry = true
					info.lock.Unlock()
					continue
				}
				info.detachNotifyDriverContext()
				info.driverCtx = nil
			}

			if err := driver.AddLink(rootOverlayNamespaceLinkName, netID); err != nil {
				e.log.Warnf("failed to add link to net %v. [driverType = %v] (err = \"%v\")", netID, driverType)
				needRetry = true
				info.lock.Unlock()
				continue
			}
			info.driverCtx = drvCtx

			info.lock.Unlock()

			changed = true
		}

		if needRetry {
			e.delayActivateDriver(driverType, driver, time.Second*5)
		}

		if changed {
			// some networks activated. trigger republishment.
			e.republishC <- republishAll
		}
	})
}
