package edgerouter

import (
	"errors"

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
	if r.metaNet.WatchKeyChanges(r.onOverlayNetworkStateChanged, gossip.DefaultNetworkEndpointKey) {
		return errors.New("cannot watch overlay metadata changes")
	}

	return nil
}

func (r *EdgeRouter) onPeerExistenceChange(peer *metanet.MetaPeer, isLeaving bool) {
	r.metaNet.SladderTxn(func(t *sladder.Transaction) bool {
		rtx, err := t.KV(peer.SladderNode(), r.overlayModelKey)
		if err != nil {
			r.log.Errorf("cannot open overlay network metadata. (err = \"%v\")", err)
			return false
		}
		nets := rtx.(*gossip.OverlayNetworksV1Txn)
		r.networkMapLearnNetworkAppeared(peer, nets.CloneCurrent())
		return false
	})
}

func (r *EdgeRouter) onPeerJoin(peer *metanet.MetaPeer) bool {
	r.onPeerExistenceChange(peer, false)
	return true
}

func (r *EdgeRouter) onPeerLeave(peer *metanet.MetaPeer) bool {
	r.onPeerExistenceChange(peer, true)
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

			route.PeerJoin(peer)
			params := paramContainer.(*gossip.CrossmeshOverlayParamV1)
			if len(params.Subnets) > 0 {
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
					if isActivityWatcher {
						watcher.PeerJoin(peer) // activate.
					}
				} else {
					// update static routes.
					oldParam := oldParamContainer.(*gossip.CrossmeshOverlayParamV1)
					toRemove := oldParam.Subnets.Clone()
					toRemove.Remove(param.Subnets...)
					if toRemove.Len() > 0 {
						route.RemoveStaticCIDRRoutes(peer, toRemove...)
					}
				}
				route.AddStaticCIDRRoutes(peer, param.Subnets...)
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
				if isActivityWatcher {
					watcher.PeerLeave(peer)
				}
			}
		case gossip.CrossmeshSymmetryRoute:
			if r.Mode() == "overlay" {
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
			if _, appeared := peerNetMap[gossip.NetworkID{
				ID:         0,
				DriverType: gossip.CrossmeshSymmetryEthernet,
			}]; appeared {
				watcher.PeerLeave(peer)
			}

		case "overlay":
			if _, appeared := peerNetMap[gossip.NetworkID{
				ID:         0,
				DriverType: gossip.CrossmeshSymmetryEthernet,
			}]; appeared {
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
