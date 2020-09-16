package metanet

import (
	"sort"
	"sync"

	"github.com/crossmesh/fabric/backend"
	gossipUtils "github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/sladder"
	"github.com/crossmesh/sladder/engine/gossip"
)

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
	n.gossip.engine = gossip.New(n.gossip.transport, gossip.WithLogger(engineLogger)).(*gossip.EngineInstance)
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
		oldEndpointIndex[oldEndpoint.Endpoint] = &oldEndpoint
	}
	var pub MetaPeerStatePublication
	var newEndpoints []MetaPeerEndpoint
	var names []string
	for _, ep := range endpointSet {
		endpoint := backend.Endpoint{
			Type:     ep.Type,
			Endpoint: ep.Endpoint,
		}
		names = append(names, gossipUtils.BuildNodeName(endpoint))
		newEndpoints = append(newEndpoints, MetaPeerEndpoint{
			Endpoint: endpoint,
			Priority: ep.Priority,
			Disabled: false,
		})
		newEndpoint := &newEndpoints[len(newEndpoints)-1]
		if oldEndpoint, hasOldEndpoint := oldEndpointIndex[endpoint]; hasOldEndpoint && oldEndpoint.Disabled {
			newEndpoint.Disabled = true
		}
	}
	sort.Slice(newEndpoints, func(i, j int) bool { return newEndpoints[i].Higher(newEndpoints[j]) }) // sort by priority.
	pub.Endpoints = newEndpoints

	// deal with names.
	if oldNames := peer.names; !stringsEqual(names, peer.names) {
		// republish Name2Peer.
		oldName2Peer := n.Publish.Name2Peer
		name2Peer := make(map[string]*MetaPeer, len(oldName2Peer)-len(oldNames)+len(names))
		for name, metaPeer := range oldName2Peer {
			name2Peer[name] = metaPeer
		}
		for _, name := range oldNames {
			delete(name2Peer, name)
		}
		for _, name := range names {
			name2Peer[name] = peer
		}
		n.Publish.Name2Peer = name2Peer
		pub.Names = names
	}

	if !pub.Trival() {
		peer.publishInterval(&pub)
	}
}

func (n *MetadataNetwork) onGossipNodeRemoved(node *sladder.Node) {
	n.lock.Lock()

	peer, hasPeer := n.peers[node]
	if !hasPeer {
		// should not hanppen.
		n.log.Warnf("[BUG!] a untracked peer left. [node = %v]", node)
		return
	}
	delete(n.peers, node)

	// republish Name2Peer.
	oldName2Peer := n.Publish.Name2Peer
	name2Peer := make(map[string]*MetaPeer, len(oldName2Peer)-len(peer.names))
	for n, p := range oldName2Peer {
		name2Peer[n] = p
	}
	for _, name := range peer.names {
		delete(name2Peer, name)
	}
	n.Publish.Name2Peer = name2Peer

	n.lock.Unlock()

	n.notifyPeerLeft(peer)
}

func (n *MetadataNetwork) onGossipNodeJoined(node *sladder.Node) {
	n.lock.Lock()
	peer := MetaPeer{Node: node}
	n.peers[node] = &peer

	// notify joined event before publish network endpoint.
	n.notifyPeerJoin(&peer)

	n.updateNetworkEndpoint(&peer, nil)

	n.lock.Unlock()

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

func (n *MetadataNetwork) notifyPeerJoin(peer *MetaPeer) {
	n.notifyPeerWatcher(&n.peerJoinWatcher, peer)
}

func (n *MetadataNetwork) notifyPeerLeft(peer *MetaPeer) {
	n.notifyPeerWatcher(&n.peerLeaveWatcher, peer)
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
		n.lock.Lock()
		defer n.lock.Unlock()
		peer, _ := n.peers[meta.Node()]
		if peer == nil {
			break
		}
		meta := meta.(sladder.KeyInsertEventMetadata)
		n.updateNetworkEndpointFromRaw(peer, meta.Value())

	case sladder.KeyDelete:
		n.lock.Lock()
		defer n.lock.Unlock()
		peer, _ := n.peers[meta.Node()]
		if peer == nil {
			break
		}
		n.updateNetworkEndpoint(peer, gossipUtils.NetworkEndpointSetV1{})

	case sladder.ValueChanged:
		n.lock.Lock()
		defer n.lock.Unlock()
		peer, _ := n.peers[meta.Node()]
		if peer == nil {
			break
		}
		meta := meta.(sladder.KeyChangeEventMetadata)
		n.updateNetworkEndpointFromRaw(peer, meta.New())
	}
}
