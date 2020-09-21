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

func (r *EdgeRouter) isValidRoutePeer(t *sladder.Transaction, peer *metanet.MetaPeer) bool {
	wantType, routeMode := gossip.UnknownOverlayDriver, r.Mode()
	switch routeMode {
	case "ethernet":
		wantType = gossip.CrossmeshSymmetryEthernet
	case "overlay":
		wantType = gossip.CrossmeshSymmetryRoute
	}
	if wantType == gossip.UnknownOverlayDriver {
		r.log.Errorf("want unknown overlay driver. [mode = %v]", routeMode)
		return false
	}

	rtx, err := t.KV(peer.SladderNode(), r.overlayModelKey)
	if err != nil {
		r.log.Errorf("cannot open overlay network metadata. (err = \"%v\")", err)
		return false
	}
	nets := rtx.(*gossip.OverlayNetworksV1Txn)
	if v1 := nets.NetworkFromID(gossip.NetworkID{
		ID: 0, DriverType: wantType,
	}); v1 != nil { // matched network.
		return true
	} else if t.KeyExists(peer.SladderNode(), r.overlayModelKey) {
		// this peer doesn't publish enough metadata so far. treat it valid.
		return true
	}

	return false
}

func (r *EdgeRouter) onPeerExistenceChange(peer *metanet.MetaPeer, isLeaving bool) {
	watcher, isActivityWatcher := r.route.(route.PeerActivityWatcher)
	if !isActivityWatcher {
		return
	}

	// capture peer memebership events for activity watcher.
	r.metaNet.SladderTxn(func(t *sladder.Transaction) bool {
		r.lock.Lock()
		defer r.lock.Unlock()

		if r.isValidRoutePeer(t, peer) {
			if isLeaving {
				// notify peer left.
				watcher.PeerJoin(peer)
			} else {
				// notify peer joins.
				watcher.PeerJoin(peer)
			}
		}
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

func (r *EdgeRouter) onOverlayNetworkStateChanged(meta sladder.KeyValueEventMetadata) bool {
	switch meta.Event() {
	case sladder.KeyDelete:
	case sladder.KeyInsert:
	case sladder.ValueChanged:
	}
	return true
}
