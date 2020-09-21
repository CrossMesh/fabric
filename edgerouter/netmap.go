package edgerouter

import (
	"errors"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/fabric/route"
	"github.com/crossmesh/sladder"
)

func (r *EdgeRouter) initializeNetworkMap() (err error) {
	// data models.
	r.overlayModel = &gossip.OverlayNetworksValidatorV1{}
	r.overlayModelKey = gossip.DefaultOverlayNetworkKey
	r.overlayModel.RegisterDriverType(gossip.CrossmeshSymmetryEthernet, gossip.CrossmeshOverlayParamV1Validator{})
	r.overlayModel.RegisterDriverType(gossip.CrossmeshSymmetryRoute, gossip.CrossmeshOverlayParamV1Validator{})
	if err = r.metaNet.RegisterDataModel(r.overlayModelKey, r.overlayModel, true, false, 0); err != nil {
		return err
	}

	// watch for peer.
	r.metaNet.RegisterPeerJoinWatcher(r.onPeerJoin)
	r.metaNet.RegisterPeerLeaveWatcher(r.onPeerLeave)
	// watch for overlay metadata changes.
	if !r.metaNet.WatchKeyChanges(r.onOverlayNetworkStateChanged, gossip.DefaultOverlayNetworkKey) {
		return errors.New("cannot watch overlay metadata changes")
	}

	return nil
}

func (r *EdgeRouter) publishLocalOverlayConfig(peer *metanet.MetaPeer, nets *gossip.OverlayNetworksV1Txn) (updated bool, err error) {
	switch m := r.Mode(); m {
	case "ethernet":
		nets.RemoveNetwork(gossip.NetworkID{
			ID:         0,
			DriverType: gossip.CrossmeshSymmetryRoute,
		})
		netID := gossip.NetworkID{
			ID:         0,
			DriverType: gossip.CrossmeshSymmetryEthernet,
		}
		if err = nets.AddNetwork(netID); err != nil {
			return false, err
		}

	case "overlay":
		nets.RemoveNetwork(gossip.NetworkID{
			ID:         0,
			DriverType: gossip.CrossmeshSymmetryEthernet,
		})
		netID := gossip.NetworkID{
			ID:         0,
			DriverType: gossip.CrossmeshSymmetryRoute,
		}
		if err = nets.AddNetwork(netID); err != nil {
			return false, err
		}

		// TODO(xutao): publish static routes.

	default:
		r.log.Errorf("Unknown working mode \"%v\". Skip publishing local overlay config for safety.", m)
		return false, nil
	}

	return nets.Updated(), nil
}

func (r *EdgeRouter) delayProcessOnPeerJoin(peer *metanet.MetaPeer, d time.Duration) {
	r.arbiter.Go(func() {
		if d > 0 {
			select {
			case <-time.After(d):
			case <-r.arbiter.Exit():
				return
			}
		} else if !r.arbiter.ShouldRun() {
			return
		}

		r.lock.Lock()
		_, hasMap := r.networkMap[peer]
		if hasMap { // peer left.
			return
		}
		r.lock.Unlock()

		r.processOnPeerJoin(peer)
	})
}

func (r *EdgeRouter) processOnPeerJoin(peer *metanet.MetaPeer) {
	var errs common.Errors

	errs.Trace(r.metaNet.SladderTxn(func(t *sladder.Transaction) bool {
		rtx, err := t.KV(peer.SladderNode(), r.overlayModelKey)
		if err != nil {
			r.log.Errorf("cannot open overlay network metadata. (err = \"%v\")", err)
			return false
		}

		nets := rtx.(*gossip.OverlayNetworksV1Txn)
		if !peer.IsSelf() {
			r.networkMapLearnNetworkAppeared(peer, nets.CloneCurrent())
			return false
		}

		updated, err := r.publishLocalOverlayConfig(peer, nets)
		if err != nil {
			errs.Trace(err)
			return false
		}

		t.DeferOnCommit(func() {
			r.log.Info("local overlay network config published.")
		})

		return updated
	}))

	if err := errs.AsError(); err != nil {
		r.log.Error("some errors raised during processing peer join event. retry later. (err = \"%v\")", err)
		r.delayProcessOnPeerJoin(peer, time.Second*5)
	}
}

func (r *EdgeRouter) onPeerJoin(peer *metanet.MetaPeer) bool {
	r.lock.Lock()
	netMap, has := r.networkMap[peer]
	if !has || netMap == nil {
		r.networkMap[peer] = make(map[gossip.NetworkID]interface{})
	}
	r.lock.Unlock()

	r.processOnPeerJoin(peer)

	return true
}

func (r *EdgeRouter) onPeerLeave(peer *metanet.MetaPeer) bool {
	r.networkMapLearnOverlayMetadataDisappeared(peer)

	r.lock.Lock()
	delete(r.networkMap, peer)
	r.lock.Unlock()

	return true
}

func (r *EdgeRouter) rebuildRoute(lock bool) {
	if lock {
		r.lock.Lock()
		defer r.lock.Unlock()
	}

	switch r.Mode() {
	case "ethernet":
		watcher, isActivityWatcher := r.route.(route.PeerActivityWatcher)
		if isActivityWatcher {
			for peer, netMap := range r.networkMap {
				if _, appeared := netMap[gossip.NetworkID{
					ID:         0,
					DriverType: gossip.CrossmeshSymmetryEthernet,
				}]; appeared {
					r.log.Infof("rebuilding route discovers peer %v.", peer)
					watcher.PeerJoin(peer)
				}
			}
		}

	case "overlay":
		route := r.route.(*route.P2PL3IPv4MeshNetworkRouter)
		for peer, netMap := range r.networkMap {
			paramContainer, appeared := netMap[gossip.NetworkID{
				ID:         0,
				DriverType: gossip.CrossmeshSymmetryEthernet,
			}]
			if !appeared {
				continue
			}

			names := peer.Names()
			route.PeerJoin(peer)
			r.log.Infof("rebuilding route discovers peer %v.", peer)
			params := paramContainer.(*gossip.CrossmeshOverlayParamV1)
			if len(params.Subnets) > 0 {
				r.log.Infof("add static route %v to peer %v.", params.Subnets, names)
				route.AddStaticCIDRRoutes(peer, params.Subnets...)
			}
		}
	}
}

func (r *EdgeRouter) networkMapLearnNetworkAppeared(peer *metanet.MetaPeer, v1 *gossip.OverlayNetworksV1) {
	r.lock.Lock()
	defer r.lock.Unlock()

	watcher, isActivityWatcher := r.route.(route.PeerActivityWatcher)

	peerNetMap, hasPeerNetMap := r.networkMap[peer]
	if !hasPeerNetMap {
		peerNetMap = make(map[gossip.NetworkID]interface{})
	}

	for netID, rawParam := range v1.Networks {
		if netID.DriverType != gossip.CrossmeshSymmetryEthernet &&
			netID.DriverType != gossip.CrossmeshSymmetryRoute { // other overlay types are not supported yet. ignore.
			continue
		}
		if netID.ID != 0 { // only ID 0 (underlay).
			continue
		}

		oldParamContainer, hasPrev := peerNetMap[netID]
		switch netID.DriverType {
		case gossip.CrossmeshSymmetryEthernet:
			param := &gossip.CrossmeshOverlayParamV1{}
			if err := param.Decode([]byte(rawParam.Params)); err != nil {
				r.log.Errorf("cannot decode new CrossmeshOverlayParamV1 structure. (err = \"%v\")", err)
				continue
			}

			if r.Mode() == "ethernet" {
				if !hasPrev {
					r.log.Infof("network %v learns a new peer %v.", netID, peer)
					if isActivityWatcher {
						watcher.PeerJoin(peer) // activate.
					}
				}
			}

			peerNetMap[netID] = param

		case gossip.CrossmeshSymmetryRoute:
			route := r.route.(*route.P2PL3IPv4MeshNetworkRouter)
			param := &gossip.CrossmeshOverlayParamV1{}
			if err := param.Decode([]byte(rawParam.Params)); err != nil {
				r.log.Errorf("cannot decode new CrossmeshOverlayParamV1 structure. (err = \"%v\")", err)
				continue
			}

			if r.Mode() == "overlay" {
				if !hasPrev {
					r.log.Infof("network %v learns a new peer %v.", netID, peer)
					if isActivityWatcher {
						watcher.PeerJoin(peer) // activate.
					}
					if param.Subnets.Len() > 0 {
						r.log.Infof("network %v adds static routes: %v --> %v.", netID, param.Subnets, peer)
						route.AddStaticCIDRRoutes(peer, param.Subnets...)
					}
				} else {
					// update static routes.
					oldParam := oldParamContainer.(*gossip.CrossmeshOverlayParamV1)
					toRemove := oldParam.Subnets.Clone()
					r.log.Info("toRemove", toRemove)
					toRemove.Remove(param.Subnets...)
					if toRemove.Len() > 0 {
						r.log.Infof("network %v removes static routes: %v --> %v.", netID, toRemove, peer)
						route.RemoveStaticCIDRRoutes(peer, toRemove...)
					}
					additions := param.Subnets.Clone()
					additions.Remove(oldParam.Subnets...)
					r.log.Info("add", additions)
					if additions.Len() > 0 {
						r.log.Infof("network %v adds static routes: %v --> %v.", netID, additions, peer)
						route.AddStaticCIDRRoutes(peer, param.Subnets...)
					}
				}
			}

			peerNetMap[netID] = param
		}
	}

	for netID := range peerNetMap {
		if _, needPreserve := v1.Networks[netID]; needPreserve {
			continue
		}
		switch netID.DriverType {
		case gossip.CrossmeshSymmetryEthernet:
			if r.Mode() == "ethernet" {
				r.log.Info("peer %v left network %v.", peer, netID)
				if isActivityWatcher {
					watcher.PeerLeave(peer)
				}
			}
		case gossip.CrossmeshSymmetryRoute:
			if r.Mode() == "overlay" {
				r.log.Info("peer %v left network %v.", peer, netID)
				if isActivityWatcher {
					watcher.PeerLeave(peer)
				}
			}
		}
		delete(peerNetMap, netID)
	}

	if !hasPeerNetMap {
		r.networkMap[peer] = peerNetMap
	}
}

func (r *EdgeRouter) networkMapLearnNetworkAppearedRaw(peer *metanet.MetaPeer, val string) {
	v1 := gossip.OverlayNetworksV1{}
	if err := v1.DecodeString(val); err != nil {
		r.log.Errorf("cannot decode new OverlayNetworksV1 structure. (err = \"%v\")", err)
		return
	}
	r.networkMapLearnNetworkAppeared(peer, &v1)
}

func (r *EdgeRouter) networkMapLearnOverlayMetadataDisappeared(peer *metanet.MetaPeer) {
	r.lock.Lock()
	defer r.lock.Unlock()

	watcher, isActivityWatcher := r.route.(route.PeerActivityWatcher)
	if !isActivityWatcher {
		return
	}

	peerNetMap, hasPeerNetMap := r.networkMap[peer]
	if !hasPeerNetMap {
		return // nothing to do.
	}

	if isActivityWatcher {
		switch r.Mode() {
		case "ethernet":
			netID := gossip.NetworkID{
				ID:         0,
				DriverType: gossip.CrossmeshSymmetryEthernet,
			}
			if _, appeared := peerNetMap[netID]; appeared {
				r.log.Infof("peer %v leaves network %v.", peer, netID)
				watcher.PeerLeave(peer)
			}

		case "overlay":
			netID := gossip.NetworkID{
				ID:         0,
				DriverType: gossip.CrossmeshSymmetryRoute,
			}
			if _, appeared := peerNetMap[netID]; appeared {
				r.log.Infof("peer %v leaves network %v.", peer, netID)
				watcher.PeerLeave(peer)
			}
		}
	}

	delete(r.networkMap, peer)
}

func (r *EdgeRouter) onOverlayNetworkStateChanged(peer *metanet.MetaPeer, meta sladder.KeyValueEventMetadata) bool {
	switch meta.Event() {
	case sladder.KeyInsert:
		meta := meta.(sladder.KeyInsertEventMetadata)
		r.networkMapLearnNetworkAppearedRaw(peer, meta.Value())
	case sladder.ValueChanged:
		meta := meta.(sladder.KeyChangeEventMetadata)
		r.networkMapLearnNetworkAppearedRaw(peer, meta.New())
	case sladder.KeyDelete:
		r.networkMapLearnOverlayMetadataDisappeared(peer)
	}
	return true
}
