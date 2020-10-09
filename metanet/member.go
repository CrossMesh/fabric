package metanet

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/common"
	gossipUtils "github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/sladder"
	"github.com/crossmesh/sladder/engine/gossip"
)

// SeedEndpoints add seed endpoints.
func (n *MetadataNetwork) SeedEndpoints(endpoints ...backend.Endpoint) (err error) {
	var errs common.Errors

	errs.Trace(n.gossip.cluster.Txn(func(t *sladder.Transaction) (changed bool) {
		changed = false
		for _, endpoint := range endpoints {
			name := gossipUtils.BuildNodeName(endpoint)
			node := t.MostPossibleNode([]string{name})
			if node != nil {
				continue
			}
			if node, err = t.NewNode(); err != nil {
				errs.Trace(err)
				return false
			}
			rtx, ierr := t.KV(node, gossipUtils.DefaultNetworkEndpointKey)
			if ierr != nil {
				errs.Trace(ierr)
				return false
			}
			eps := rtx.(*gossipUtils.NetworkEndpointsV1Txn)
			if eps.AddEndpoints(gossipUtils.NetworkEndpointV1{
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
	n.gossip.nameResolver = gossipUtils.NewPeerNameResolver()
	if n.gossip.cluster, n.gossip.self, err =
		sladder.NewClusterWithNameResolver(n.gossip.engine, n.gossip.nameResolver, sladder.Logger(logger)); err != nil {
		n.log.Fatalf("failed to initialize gossip memebership. (err = \"%v\")", err)
		return
	}

	// register basic model.
	if err = n.gossip.cluster.RegisterKey(gossipUtils.DefaultNetworkEndpointKey,
		n.gossip.engine.WrapVersionKVValidator(gossipUtils.NetworkEndpointsValidatorV1{}), true, 0); err != nil {
		n.log.Fatalf("failed to register gossip model NetworkEndpointsValidatorV1. [gossip key = \"%v\"] (err = \"%v\")",
			gossipUtils.DefaultNetworkEndpointKey, err.Error())
		return
	}

	// watch for membership.
	n.gossip.cluster.Watch(n.onGossipNodeEvent)
	n.gossip.cluster.Keys(gossipUtils.DefaultNetworkEndpointKey).Watch(n.onNetworkEndpointEvent)

	n.self = &MetaPeer{
		Node:   n.gossip.self,
		isSelf: true,
	}
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
	v1 := gossipUtils.NetworkEndpointsV1{}
	if err := v1.DecodeString(v); err != nil {
		n.log.Errorf("failed to decode NetworkEndpointsV1. (err = \"%v\")", err)
		return
	}
	if v1.Endpoints == nil {
		v1.Endpoints = gossipUtils.NetworkEndpointSetV1{}
	}
	n.updateNetworkEndpoint(peer, v1.Endpoints)
}

func (n *MetadataNetwork) updateNetworkEndpoint(peer *MetaPeer, endpointSet gossipUtils.NetworkEndpointSetV1) {
	if endpointSet == nil { // endpoint not given. get latest one.
		if err := n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(peer.Node, gossipUtils.DefaultNetworkEndpointKey)
			if err != nil {
				n.log.Errorf("cannot load gossip network endpoint. network endpoint fails to be updated. (err = \"%v\")", err)
				return false
			}
			eps := rtx.(*gossipUtils.NetworkEndpointsV1Txn)
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
		names = append(names, gossipUtils.BuildNodeName(endpoint))
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
		peer = &MetaPeer{Node: node}
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
		n.updateNetworkEndpoint(peer, gossipUtils.NetworkEndpointSetV1{})

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
	id := atomic.AddUint32(&n.configID, 1)
	n.delayPublishEndpoint(id, false) // submit.
}

func (n *MetadataNetwork) delayPublishEndpoint(id uint32, delay bool) {
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

		if id < n.configID {
			return
		}

		oldEndpoints := make(map[backend.Endpoint]*MetaPeerEndpoint)
		for _, endpoint := range n.self.Endpoints {
			oldEndpoints[endpoint.Endpoint] = endpoint
		}

		var (
			newEndpoints []*gossipUtils.NetworkEndpointV1
		)

		localPublish := make([]*MetaPeerEndpoint, 0, len(n.backends))
		for endpoint, backend := range n.backends {
			oldPublish, hasOldPublish := oldEndpoints[endpoint]
			if hasOldPublish && oldPublish != nil {
				localPublish = append(localPublish, &MetaPeerEndpoint{
					Endpoint: endpoint,
					Priority: backend.Priority(),
					Disabled: oldPublish.Disabled,
				})
				if oldPublish.Disabled {
					continue // do not gossip disabled endpoint.
				}
			} else {
				localPublish = append(localPublish, &MetaPeerEndpoint{
					Endpoint: endpoint,
					Priority: backend.Priority(),
					Disabled: false,
				})
			}

			newEndpoints = append(newEndpoints, &gossipUtils.NetworkEndpointV1{
				Type:     endpoint.Type,
				Endpoint: endpoint.Endpoint,
				Priority: backend.Priority(),
			})
		}

		var errs common.Errors

		errs.Trace(n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(n.gossip.cluster.Self(), gossipUtils.DefaultNetworkEndpointKey)
			if err != nil {
				errs.Trace(err)
				return false
			}
			eps := rtx.(*gossipUtils.NetworkEndpointsV1Txn)
			eps.UpdateEndpoints(newEndpoints...)
			return true
		}))
		if err := errs.AsError(); err != nil {
			n.log.Errorf("failed to commit transaction for endpoint publication. (err = \"%v\")", err)
			n.delayPublishEndpoint(id, true)
			return
		}
		n.log.Infof("gossip publish endpoints %v", newEndpoints)

		// publish locally.
		typePublish := make(map[backend.Type]backend.Backend)
		sort.Slice(localPublish, func(i, j int) bool { return localPublish[i].Higher(*localPublish[j]) }) // sort by priority.
		for _, endpoint := range localPublish {
			candidate, hasBackend := n.backends[endpoint.Endpoint]
			if !hasBackend {
				// should not happen.
				n.log.Warnf("[BUG!] publish an endpoint with a missing backend in local backend store.")
				n.RepublishEndpoint() // for safefy.
				continue
			}

			rb, hasPublish := n.Publish.Type2Backend.Load(endpoint.Type)
			if hasPublish && rb != nil {
				currentBackend, valid := rb.(backend.Backend)
				if !valid || currentBackend == nil {
					n.log.Warnf("[BUG!] published a invalid backend. [nil = %v]", currentBackend == nil)
					n.Publish.Type2Backend.Delete(endpoint.Type)
				} else if currentBackend == candidate && endpoint.Disabled { // disabled
					n.Publish.Type2Backend.Delete(endpoint.Type)
				} else { // preserve the currently selected.
					typePublish[endpoint.Type] = currentBackend
				}
			}

			if !endpoint.Disabled {
				if _, hasCandidate := typePublish[endpoint.Type]; !hasCandidate {
					typePublish[endpoint.Type] = candidate // select this candidate.
				}
			}
		}
		for ty, backend := range typePublish { // now publish backends by type.
			n.Publish.Type2Backend.Store(ty, backend)
		}
		// publish to MetaPeer.
		n.self.publishInterval(func(interval *MetaPeerStatePublication) bool {
			interval.Endpoints = localPublish
			return true
		})
	})
}
