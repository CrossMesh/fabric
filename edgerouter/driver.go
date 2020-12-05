package edgerouter

import (
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/metanet"
	arbit "github.com/sunmxt/arbiter"
)

type driverMessager struct {
	driverType driver.OverlayDriverType
}

func (m *driverMessager) Send(peer *metanet.MetaPeer, msg []byte) {
}

func (m *driverMessager) WatchMessage(handle func(*metanet.Message) bool) {
}

type driverNetworkMap struct {
	lock sync.RWMutex

	info *networkInfo

	membershipChangesWatcher map[*driver.PeerEventHandler]struct{}

	localOptions        map[string][]byte
	remoteOptions       map[*metanet.MetaPeer]map[string][]byte
	remoteOptionWatcher map[*driver.RemoteOptionMapWatcher]struct{}
}

func newDriverNerworkMap(info *networkInfo) *driverNetworkMap {
	return &driverNetworkMap{
		info:                     info,
		membershipChangesWatcher: make(map[*driver.PeerEventHandler]struct{}),
		localOptions:             map[string][]byte{},
		remoteOptions:            make(map[*metanet.MetaPeer]map[string][]byte),
		remoteOptionWatcher:      make(map[*driver.RemoteOptionMapWatcher]struct{}),
	}
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

func (m *driverNetworkMap) UnderlayID(p *metanet.MetaPeer) int32 { return m.info.UnderlayID(p) }
func (m *driverNetworkMap) WatchUnderlayID(handler driver.UnderlayIDWatcher) {
	m.info.WatchUnderlayID(handler)
}

func (m *driverNetworkMap) PeerIPs(p *metanet.MetaPeer) (public, private common.IPNetSet) {
	return m.info.PeerIPs(p)
}

func (m *driverNetworkMap) WatchPeerIPs(handler driver.UnderlayIPWatcher) {
	m.info.WatchPeerIPs(handler)
}

func (m *driverNetworkMap) PeerJoin(p *metanet.MetaPeer) {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, hasPeer := m.remoteOptions[p]
	if hasPeer {
		return
	}

	// notify.
	for handler := range m.membershipChangesWatcher {
		if !(*handler)(p, driver.PeerLeft) { // canceled?
			delete(m.membershipChangesWatcher, handler)
		}
	}
	m.remoteOptions[p] = nil
}

func (m *driverNetworkMap) PeerLeft(p *metanet.MetaPeer) {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, hasPeer := m.remoteOptions[p]
	if !hasPeer {
		return
	}

	// notify.
	for handler := range m.membershipChangesWatcher {
		if !(*handler)(p, driver.PeerLeft) { // canceled?
			delete(m.membershipChangesWatcher, handler)
		}
	}
	delete(m.remoteOptions, p)
}

func (m *driverNetworkMap) ProcessNewRemoteOptions(p *metanet.MetaPeer, opts map[string][]byte) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, hasPeer := m.remoteOptions[p]; !hasPeer {
		return // ignore remote options for absent peer.
	}

	newOpts := make(map[string][]byte, len(opts))
	for k, v := range opts {
		newOpts[k] = v
	}
	m.remoteOptions[p] = newOpts

	for handler := range m.remoteOptionWatcher {
		if !(*handler)(p, newOpts) {
			delete(m.remoteOptionWatcher, handler)
		}
	}
}

type driverContext struct {
	router *EdgeRouter
	store  common.SubpathStore

	lock sync.RWMutex

	driverType driver.OverlayDriverType
	driver     driver.OverlayDriver
	messager   *driverMessager

	networkMap map[int32]*driverNetworkMap
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

func (e *EdgeRouter) ensureNetworkNamespace(netID int32) (string, error) {
	return "", nil
	if e.underlay.ID != netID {
		// overlay
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

			netns, err := e.ensureNetworkNamespace(netID)
			if err != nil {
				e.log.Warnf("failed to ensure namespace for net %v. [driverType = %v] (err = \"%v\")", netID, driverType)
				needRetry = true
				continue
			}

			info.lock.Lock()

			oldCtx := info.driverCtx
			if oldCtx != nil {
				if err = oldCtx.driver.DelLink(rootOverlayNamespaceLinkName, netns, netID); err != nil {
					e.log.Warnf("failed to remove link from net %v. [driverType = %v, netns = %v] (err = \"%v\")", netID, netns, driverType)
					needRetry = true
					info.lock.Unlock()
					continue
				}
				info.detachNotifyDriverContext()
				info.driverCtx = nil
			}

			if err = driver.AddLink(rootOverlayNamespaceLinkName, netns, netID); err != nil {
				e.log.Warnf("failed to add link to net %v. [driverType = %v, netns = %v] (err = \"%v\")", netID, netns, driverType)
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
