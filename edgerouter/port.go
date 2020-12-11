package edgerouter

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/metanet"
)

// UnderlayID reports current underlay.
func (r *EdgeRouter) UnderlayID() int32 {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.underlay.ID
}

// SetUnderlayIDs sets underlay.
func (r *EdgeRouter) SetUnderlayIDs(netID int32) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if netID == r.underlay.ID {
		return nil
	}

	return r.doStoreWriteTxn(func(tx common.StoreTxn) (bool, error) {
		path := []string{"underlay_id"}
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(netID))
		data := buf[:]

		if err := tx.Set(path, data); err != nil {
			return false, err
		}
		tx.OnCommit(func() { r.underlay.ID = netID })
		return true, nil
	})
}

// AddUnderlayIPs adds static underlay IPs.
func (r *EdgeRouter) AddUnderlayIPs(ips ...*net.IPNet) error {
	if len(ips) < 1 {
		return nil
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	newSet := r.underlay.staticIPs.Clone()
	changed := newSet.Merge(ips)
	if !changed {
		return nil
	}

	return r.commitNewUnderlayIPs(newSet)
}

// RemoveUnderlayIPs removes static underlay IPs.
func (r *EdgeRouter) RemoveUnderlayIPs(ips ...*net.IPNet) error {
	if len(ips) < 1 {
		return nil
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	newSet := r.underlay.staticIPs.Clone()
	changed := newSet.Remove(ips...)
	if !changed {
		return nil
	}

	return r.commitNewUnderlayIPs(newSet)
}

// AddStaticOverlayIPs adds static overlay IPs.
func (r *EdgeRouter) AddStaticOverlayIPs(netID int32, ips common.IPNetSet) error {
	r.lock.RLock()
	defer r.lock.RUnlock()

	r.modifyOverlayIPs(netID, func(newIPs *common.IPNetSet) bool {
		return newIPs.Merge(ips)
	})

	return nil
}

// RemoveStaticOverlayIPs removes static overlay IPs.
func (r *EdgeRouter) RemoveStaticOverlayIPs(netID int32, ips common.IPNetSet) error {
	r.lock.RLock()
	defer r.lock.RUnlock()

	r.modifyOverlayIPs(netID, func(newIPs *common.IPNetSet) bool {
		return newIPs.Remove(ips...)
	})

	return nil
}

// PublicIPs reports public underlay IPs.
func (r *EdgeRouter) PublicIPs() (ips common.IPNetSet) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.filterUnderlayIPs(func(ip *net.IPNet) bool {
		for _, private := range common.PrivateUnicastIPNets {
			if private.Contains(ip.IP) {
				return false
			}
		}
		return true
	})
}

// PrivateIPs reports private underlay IPs.
func (r *EdgeRouter) PrivateIPs() (ips common.IPNetSet) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.filterUnderlayIPs(func(ip *net.IPNet) bool {
		for _, private := range common.PrivateUnicastIPNets {
			if !private.Contains(ip.IP) {
				return false
			}
		}
		return true
	})
}

// RegisterOverlayDriver registers overlay driver.
func (r *EdgeRouter) RegisterOverlayDriver(drv driver.OverlayDriver) error {
	if drv == nil {
		r.log.Warn("try to register nil OverlayDriver. skip.")
		return nil
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	typeID := drv.Type()
	oldDrv, _ := r.drivers[typeID]
	if oldDrv != nil {
		err := fmt.Errorf("try to register duplicated overlay driver with type %v", typeID)
		r.log.Warn(err)
		return err
	}

	path := []string{"driver", strconv.FormatUint(uint64(drv.Type()), 10), "data"}
	ctx := newEmptyDriverContext(r)
	ctx.driver = drv
	ctx.store = common.SubpathStore{
		Store:  r.store,
		Prefix: path,
	}
	ctx.driverType = typeID
	ctx.messager = &driverMessager{
		driverType: typeID,
	}
	ctx.logger = r.log.WithField("driver", typeID)

	if err := drv.Init(ctx); err != nil {
		r.log.Errorf("failed to initialize driver with type %v. (err = \"%v\")", typeID, err)
		return err
	}

	r.drivers[typeID] = ctx

	r.delayActivateDriver(typeID, drv, 0)

	return nil
}

// PeerUnderlayID reports underlay ID of peer.
func (r *EdgeRouter) PeerUnderlayID(p *metanet.MetaPeer) int32 {
	if p == nil {
		return -1
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	id, exists := r.underlay.ids[p]
	if !exists {
		return -1
	}
	return id
}

// PeerIPs reports underlay IPs of peer.
func (r *EdgeRouter) PeerIPs(p *metanet.MetaPeer) (public, private common.IPNetSet) {
	if p == nil {
		return nil, nil
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	ips, _ := r.underlay.ips[p]
	if len(ips) < 1 {
		return nil, nil
	}

	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() {
			continue
		}
		for _, privNet := range common.PrivateUnicastIPNets {
			if privNet.Contains(ip.IP) {
				private = append(private, ip)
			} else {
				public = append(public, ip)
			}
		}
	}

	public.Build()
	private.Build()

	return
}

// VirtualDo runs process inside given virtualied network environment.
func (r *EdgeRouter) VirtualDo(netID int32, do func(underlayID, netID int32) error) error {
	return <-r.submitVirtualDo(netID, false, func(ctx *virtualNetworkDoContext) error {
		return do(ctx.underlay.id, ctx.current.id)
	})
}
