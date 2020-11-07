package metanet

import (
	"sort"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/metanet/backend"
	gmodel "github.com/crossmesh/fabric/metanet/gossip"
	"github.com/crossmesh/sladder"
	"github.com/crossmesh/sladder/engine/gossip"
)

// SeedEndpoints add seed endpoints.
func (n *MetadataNetwork) SeedEndpoints(endpoints ...backend.Endpoint) (err error) {
	var errs common.Errors

	errs.Trace(n.gossip.cluster.Txn(func(t *sladder.Transaction) (changed bool) {
		changed = false
		for _, endpoint := range endpoints {
			name := gmodel.BuildNodeName(endpoint)
			node := t.MostPossibleNode([]string{name})
			if node != nil {
				continue
			}
			if node, err = t.NewNode(); err != nil {
				errs.Trace(err)
				return false
			}
			rtx, ierr := t.KV(node, gmodel.DefaultNetworkEndpointKey)
			if ierr != nil {
				errs.Trace(ierr)
				return false
			}
			eps := rtx.(*gmodel.NetworkEndpointsV1Txn)
			if eps.AddEndpoints(gmodel.NetworkEndpointV1{
				Type:     endpoint.Type,
				Endpoint: endpoint.Endpoint,
				Priority: 0,
			}) {
				changed = true
			}
		}
		return
	}, sladder.MembershipModification()))

	return errs.AsError()
}

// UnregisterPeerJoinWatcher registers peer handler for peer joining event.
func (n *MetadataNetwork) UnregisterPeerJoinWatcher(watch PeerHandler) {
	n.registerPeerHandler(&n.peerJoinWatcher, watch)
}

// UnregisterPeerLeaveWatcher registers peer handler for peer leaving event.
func (n *MetadataNetwork) UnregisterPeerLeaveWatcher(watch PeerHandler) {
	n.registerPeerHandler(&n.peerLeaveWatcher, watch)
}

// RegisterPeerJoinWatcher registers peer handler for peer joining event.
func (n *MetadataNetwork) RegisterPeerJoinWatcher(watch PeerHandler) {
	n.registerPeerHandler(&n.peerJoinWatcher, watch)
}

// RegisterPeerLeaveWatcher registers peer handler for peer leaving event.
func (n *MetadataNetwork) RegisterPeerLeaveWatcher(watch PeerHandler) {
	n.registerPeerHandler(&n.peerLeaveWatcher, watch)
}

func (n *MetadataNetwork) registerPeerHandler(registry *sync.Map, watch PeerHandler) {
	n.lock.Lock()
	defer n.lock.Unlock()
	registry.Store(&watch, watch)
}

func (n *MetadataNetwork) unregisterPeerHandler(registry *sync.Map, watch PeerHandler) {
	n.lock.Lock()
	defer n.lock.Unlock()
	registry.Delete(&watch)
}

func (n *MetadataNetwork) initializeMembership() (err error) {
	if n.gossip.cluster != nil {
		return nil
	}

	defer func() {
		if err != nil {
			n.gossip.cluster, n.gossip.self, n.gossip.engine = nil, nil, nil
		}
	}()

	n.gossip.transport = n.newGossipEngineTransport(1024)

	logger := n.log.WithField("submodule", "sladder")
	engineLogger := n.log.WithField("submodule", "sladder/gossip")
	n.gossip.engine = gossip.New(n.gossip.transport,
		gossip.WithLogger(engineLogger),
		gossip.WithGossipPeriod(time.Second*3)).(*gossip.EngineInstance)
	n.gossip.nameResolver = gmodel.NewPeerNameResolver()
	if n.gossip.cluster, n.gossip.self, err =
		sladder.NewClusterWithNameResolver(n.gossip.engine, n.gossip.nameResolver, sladder.Logger(logger)); err != nil {
		n.log.Fatalf("failed to initialize gossip memebership. (err = \"%v\")", err)
		return
	}

	// register basic model.
	if err = n.gossip.cluster.RegisterKey(gmodel.DefaultNetworkEndpointKey,
		n.gossip.engine.WrapVersionKVValidator(gmodel.NetworkEndpointsValidatorV1{}), true, 0); err != nil {
		n.log.Fatalf("failed to register gossip model NetworkEndpointsValidatorV1. [gossip key = \"%v\"] (err = \"%v\")",
			gmodel.DefaultNetworkEndpointKey, err.Error())
		return
	}

	// watch for membership.
	n.gossip.cluster.Watch(n.onGossipNodeEvent)
	n.gossip.cluster.Keys(gmodel.DefaultNetworkEndpointKey).Watch(n.onNetworkEndpointEvent)

	n.self = newMetaPeer(n.gossip.self, n.log, true)
	n.peers[n.gossip.self] = n.self

	n.Publish.Self = n.self

	return nil
}

func stringsEqual(n1s, n2s []string) bool {
	if len(n1s) != len(n2s) {
		return false
	}
	if len(n1s) == 0 {
		return true
	}
	sort.Strings(n1s)
	sort.Strings(n2s)
	for idx := range n1s {
		if n1s[idx] != n2s[idx] {
			return false
		}
	}
	return true
}

func (n *MetadataNetwork) updateNetworkEndpointFromRaw(peer *MetaPeer, v string) {
	v1 := gmodel.NetworkEndpointsV1{}
	if err := v1.DecodeString(v); err != nil {
		n.log.Errorf("failed to decode NetworkEndpointsV1. (err = \"%v\")", err)
		return
	}
	if v1.Endpoints == nil {
		v1.Endpoints = gmodel.NetworkEndpointSetV1{}
	}
	n.updateNetworkEndpoint(peer, v1.Endpoints)
}

func (n *MetadataNetwork) updateNetworkEndpoint(peer *MetaPeer, endpointSet gmodel.NetworkEndpointSetV1) {
	if endpointSet == nil { // endpoint not given. get latest one.
		if err := n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(peer.Node, gmodel.DefaultNetworkEndpointKey)
			if err != nil {
				n.log.Errorf("cannot load gossip network endpoint. network endpoint fails to be updated. (err = \"%v\")", err)
				return false
			}
			eps := rtx.(*gmodel.NetworkEndpointsV1Txn)
			endpointSet = eps.GetEndpoints()

			return false
		}); err != nil {
			n.log.Errorf("got a transaction failure when getting latest network endpoints. (err = \"%v\")", err)
		}
	}
	if endpointSet == nil {
		return
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	// deal with endpoints.
	oldEndpoints, oldEndpointIndex := peer.Endpoints, make(map[backend.Endpoint]*MetaPeerEndpoint)
	for _, oldEndpoint := range oldEndpoints {
		oldEndpointIndex[oldEndpoint.Endpoint] = oldEndpoint
	}
	var pub MetaPeerStatePublication
	var newEndpoints []*MetaPeerEndpoint
	var names []string
	for _, ep := range endpointSet {
		endpoint := backend.Endpoint{
			Type:     ep.Type,
			Endpoint: ep.Endpoint,
		}
		names = append(names, gmodel.BuildNodeName(endpoint))
		newEndpoints = append(newEndpoints, &MetaPeerEndpoint{
			Endpoint: endpoint,
			Priority: ep.Priority,
			Disabled: false,
		})
		newEndpoint := newEndpoints[len(newEndpoints)-1]
		if oldEndpoint, hasOldEndpoint := oldEndpointIndex[endpoint]; hasOldEndpoint && oldEndpoint.Disabled {
			newEndpoint.Disabled = true
		}
	}
	sort.Slice(newEndpoints, func(i, j int) bool { return newEndpoints[i].Higher(*newEndpoints[j]) }) // sort by priority.
	pub.Endpoints = newEndpoints

	// deal with names.
	if oldNames := peer.names; !stringsEqual(names, peer.names) {
		n.log.Debug("recalc names: old = ", peer.names, " new = ", names, " for node = ", peer)

		// start to republish Name2Peer.

		oldName2Peer := n.Publish.Name2Peer
		name2Peer := make(map[string]*MetaPeer, len(oldName2Peer)-len(oldNames)+len(names))
		for name, metaPeer := range oldName2Peer {
			name2Peer[name] = metaPeer
		}
		for _, name := range oldNames {
			actual, has := name2Peer[name]
			if has && actual == peer {
				delete(name2Peer, name)
			}
		}

		// try to resolve name conflict.
		for node := range n.nameConflictNodes {
			allResolved := true
			if !node.left {
				for _, name := range node.names {
					actual, has := name2Peer[name]
					if has {
						if actual == peer {
							continue
						}
						if actual != nil && !actual.left {
							allResolved = false // conflicts persists.
							continue
						}
					}
					n.log.Warnf("conflict name \"%v\" of peer %v is recovered.", name, node)
					name2Peer[name] = node
				}
			}
			if allResolved {
				delete(n.nameConflictNodes, node)
			}
		}

		// assign new changes.
		conflict := false
		for _, name := range names {
			actual, has := name2Peer[name]
			if has && actual != nil && actual != peer && !actual.left {
				conflict = true
				n.log.Warnf("name \"%v\" of peer %v is temporarily removed due to name conflict with peer %v. ", name, peer, actual)
				continue
			}
			name2Peer[name] = peer
		}
		if conflict {
			n.nameConflictNodes[peer] = struct{}{}
		}

		n.log.Debug("republish name-to-peer mapping: ", name2Peer)
		n.Publish.Name2Peer = name2Peer
		pub.Names = names
	}

	if !pub.Trival() {
		n.log.Debugf("republish peer %v interval: %v", peer.Names(), &pub)
		peer.publishInterval(func(interval *MetaPeerStatePublication) bool {
			interval.Endpoints = pub.Endpoints
			interval.Names = pub.Names
			return true
		})
	}
}

func (n *MetadataNetwork) onGossipNodeRemoved(node *sladder.Node) {
	n.lock.Lock()

	peer, hasPeer := n.peers[node]
	if !hasPeer {
		// should not hanppen.
		n.log.Warnf("[BUG!] a untracked peer left. [node = %v]", node)
		n.lock.Unlock()
		return
	}
	peer.left = true // lazy deletion.
	delete(n.peers, node)

	n.lock.Unlock()

	n.notifyPeerWatcher(&n.peerLeaveWatcher, peer)

	n.log.Infof("peer %v left.", node.Names())
}

func (n *MetadataNetwork) onGossipNodeJoined(node *sladder.Node) {
	n.lock.Lock()
	peer, hasPeer := n.peers[node]
	if !hasPeer || peer.left {
		peer = newMetaPeer(node, n.log, node == n.gossip.self)
		n.peers[node] = peer
	}
	n.lock.Unlock()

	n.updateNetworkEndpoint(peer, nil)
	n.notifyPeerWatcher(&n.peerJoinWatcher, peer)

	n.log.Infof("peer %v joins", node.Names())
}

func (n *MetadataNetwork) notifyPeerWatcher(set *sync.Map, peer *MetaPeer) {
	set.Range(func(k, v interface{}) bool {
		handler, isPeerHandler := v.(PeerHandler)
		if !isPeerHandler || handler == nil {
			set.Delete(k)
			return true
		}
		if !handler(peer) {
			set.Delete(k)
		}
		return true
	})
}

func (n *MetadataNetwork) onGossipNodeEvent(ctx *sladder.ClusterEventContext, event sladder.Event, node *sladder.Node) {
	switch event {
	case sladder.NodeJoined:
		n.onGossipNodeJoined(node)

	case sladder.NodeRemoved:
		n.onGossipNodeRemoved(node)
	}
}

func (n *MetadataNetwork) onNetworkEndpointEvent(ctx *sladder.WatchEventContext, meta sladder.KeyValueEventMetadata) {
	switch meta.Event() {
	case sladder.KeyInsert:
		n.lock.RLock()
		peer, _ := n.peers[meta.Node()]
		n.lock.RUnlock()
		if peer == nil {
			break
		}
		meta := meta.(sladder.KeyInsertEventMetadata)
		n.updateNetworkEndpointFromRaw(peer, meta.Value())

	case sladder.KeyDelete:
		n.lock.RLock()
		peer, _ := n.peers[meta.Node()]
		n.lock.RUnlock()
		if peer == nil {
			break
		}
		n.updateNetworkEndpoint(peer, gmodel.NetworkEndpointSetV1{})

	case sladder.ValueChanged:
		n.lock.RLock()
		peer, _ := n.peers[meta.Node()]
		n.lock.RUnlock()
		if peer == nil {
			break
		}
		meta := meta.(sladder.KeyChangeEventMetadata)
		n.updateNetworkEndpointFromRaw(peer, meta.New())
	}
}

// RepublishEndpoint republish Endpoints.
func (n *MetadataNetwork) RepublishEndpoint() {
	n.lock.Lock()
	defer n.lock.Unlock()

	epoch := n.epoch + 1
	n.delayPublishEndpoint(epoch, false) // submit.
	n.epoch = epoch
}

func (n *MetadataNetwork) delayPublishEndpoint(epoch uint32, delay bool) {
	n.arbiters.main.Go(func() {
		if delay {
			select {
			case <-time.After(time.Second * 5):
			case <-n.arbiters.main.Exit():
				return
			}
		}

		n.lock.Lock()
		defer n.lock.Unlock()

		if epoch < n.Publish.Epoch {
			return
		}

		oldEndpoints := make(map[backend.Endpoint]*MetaPeerEndpoint)
		for _, endpoint := range n.self.Endpoints {
			oldEndpoints[endpoint.Endpoint] = endpoint
		}

		var newEndpoints []*gmodel.NetworkEndpointV1

		localPublish := make([]*MetaPeerEndpoint, 0, len(n.backendManagers))
		newBackends := map[backend.Endpoint]*backendPublish{}

		for ty, mgr := range n.backendManagers {
			for _, ep := range mgr.ListActiveEndpoints() {
				endpoint := backend.Endpoint{
					Type: ty, Endpoint: ep,
				}
				backend := mgr.GetBackend(ep)
				if backend == nil {
					n.log.Warn("[BUG!] got nil active backend.")
					continue
				}

				priority := n.getEndpointPriority(endpoint)
				oldPublish, hasOldPublish := oldEndpoints[endpoint]
				if hasOldPublish && oldPublish != nil {
					localPublish = append(localPublish, &MetaPeerEndpoint{
						Endpoint: endpoint,
						Priority: priority,
						Disabled: oldPublish.Disabled,
					})
					if oldPublish.Disabled {
						continue // do not gossip disabled endpoint.
					}
				} else {
					localPublish = append(localPublish, &MetaPeerEndpoint{
						Endpoint: endpoint,
						Priority: priority,
						Disabled: false,
					})
				}

				newEndpoints = append(newEndpoints, &gmodel.NetworkEndpointV1{
					Type:     endpoint.Type,
					Endpoint: endpoint.Endpoint,
					Priority: priority,
				})
				newBackends[endpoint] = &backendPublish{
					Backend:  backend,
					Priority: priority,
				}
			}
		}

		var errs common.Errors

		errs.Trace(n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(n.gossip.cluster.Self(), gmodel.DefaultNetworkEndpointKey)
			if err != nil {
				errs.Trace(err)
				return false
			}
			eps := rtx.(*gmodel.NetworkEndpointsV1Txn)
			eps.UpdateEndpoints(newEndpoints...)
			return true
		}))
		if err := errs.AsError(); err != nil {
			n.log.Errorf("failed to commit transaction for endpoint publication. (err = \"%v\")", err)
			n.delayPublishEndpoint(epoch, true)
			return
		}
		n.log.Infof("gossip publish endpoints %v", newEndpoints)

		// publish to MetaPeer.
		n.self.publishInterval(func(interval *MetaPeerStatePublication) bool {
			interval.Endpoints = localPublish
			return true
		})

		// publish backends.
		n.Publish.Backends = newBackends
		n.Publish.Epoch = epoch
	})
}
