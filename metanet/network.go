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
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

// KeyReservedError indicates a invalid key for reservation reason.
type KeyReservedError struct {
	key string
}

func (e *KeyReservedError) Error() string {
	return "key \"" + e.key + "\" is reserved by framework."
}

// PeerHandler accepts a MetaPeer and does something.
type PeerHandler func(peer *MetaPeer) bool

// MetaPeerEndpoint contains a network endpoint.
type MetaPeerEndpoint struct {
	backend.Endpoint
	Priority uint32
	Disabled bool
}

// Higher reports whether this endpoint has higher priority.
func (e MetaPeerEndpoint) Higher(x MetaPeerEndpoint) bool {
	if x.Disabled != e.Disabled {
		return !x.Disabled
	}
	if x.Priority != e.Priority {
		return e.Priority > x.Priority
	}
	if x.Type != e.Type {
		return e.Type < x.Type
	}
	return e.Endpoint.Endpoint < x.Endpoint.Endpoint
}

// Clone makes a deep copy.
func (e *MetaPeerEndpoint) Clone() MetaPeerEndpoint {
	new := MetaPeerEndpoint{}
	new = *e
	return new
}

// MetadataNetwork implements simple decentalized communication metadata network.
type MetadataNetwork struct {
	messageHandlers  sync.Map // map[uint16]MessageHandler
	peerLeaveWatcher sync.Map // map[uintptr]PeerHandler
	peerJoinWatcher  sync.Map // map[uintptr]PeerHandler

	lock    sync.RWMutex
	arbiter *arbit.Arbiter
	log     *logging.Entry

	quitChan chan struct{}

	gossip struct {
		cluster      *sladder.Cluster
		engine       *gossip.EngineInstance
		transport    *gossipEngineTransport
		nameResolver *gossipUtils.PeerNameResolver
		self         *sladder.Node
	}
	self  *MetaPeer
	peers map[*sladder.Node]*MetaPeer

	backends map[backend.Endpoint]backend.Backend
	configID uint32

	Publish struct {
		Name2Peer    map[string]*MetaPeer // (COW)
		Self         *MetaPeer
		Type2Backend sync.Map // map[backend.Type]backend.Backend
	}
}

// NewMetadataNetwork creates a metadata network.
func NewMetadataNetwork(arbiter *arbit.Arbiter, log *logging.Entry) (n *MetadataNetwork, err error) {
	n = &MetadataNetwork{
		peers:    make(map[*sladder.Node]*MetaPeer),
		backends: make(map[backend.Endpoint]backend.Backend),
		quitChan: make(chan struct{}),
	}

	n.arbiter = arbit.NewWithParent(arbiter)
	if log == nil {
		n.log = logging.WithField("module", "metadata_network")
	} else {
		n.log = log
	}
	if err = n.initializeMembership(); err != nil {
		return nil, err
	}
	n.arbiter.Go(func() {
		select {
		case <-n.arbiter.Exit():
			n.Close()
		case <-n.quitChan:
		}
	})

	return n, nil
}

// RegisterDataModel registers new data model to sladder framework.
func (n *MetadataNetwork) RegisterDataModel(
	key string,
	validator sladder.KVValidator,
	forceReplace bool,
	flags uint32) error {
	if key == gossipUtils.DefaultNetworkEndpointKey {
		return &KeyReservedError{key: key}
	}
	return n.gossip.cluster.RegisterKey(key, validator, forceReplace, flags)
}

// SladderTxn executes transaction on Sladder framework.
func (n *MetadataNetwork) SladderTxn(do func(t *sladder.Transaction) bool) error {
	return n.gossip.cluster.Txn(do)
}

// Close quits from metadata network.
func (n *MetadataNetwork) Close() {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.gossip.cluster.Quit()

	for endpoint, backend := range n.backends {
		backend.Shutdown()
		delete(n.backends, endpoint)
	}
	n.Publish.Type2Backend.Range(func(k, v interface{}) bool {
		n.Publish.Type2Backend.Delete(k)
		return true
	})

	close(n.quitChan)
}

func (n *MetadataNetwork) delayLocalEndpointCreation(id uint32, creators ...backend.BackendCreator) {
	n.arbiter.Go(func() {
		select {
		case <-time.After(time.Second * 5):
		case <-n.arbiter.Exit():
			return
		}

		n.lock.Lock()
		defer n.lock.Unlock()

		if id < n.configID {
			return
		}

		failCreation, changed := []backend.BackendCreator(nil), false
		for _, creator := range creators {
			endpoint := backend.Endpoint{
				Type:     creator.Type(),
				Endpoint: creator.Publish(),
			}

			// new.
			new, err := creator.New(n.arbiter, nil)
			if err != nil {
				n.log.Errorf("failed to create backend %v:%v. (err = \"%v\")", creator.Type().String(), creator.Publish, err)
				failCreation = append(failCreation, creator)
			} else {
				n.backends[endpoint] = new
				new.Watch(n.receiveRemote)
				changed = true
			}
		}

		if len(failCreation) > 0 {
			n.delayLocalEndpointCreation(id, failCreation...)
		}

		if changed {
			n.delayPublishEndpoint(id, false)
		}
	})
}

// RepublishEndpoint republish Endpoints.
func (n *MetadataNetwork) RepublishEndpoint() {
	id := atomic.AddUint32(&n.configID, 1)
	n.delayPublishEndpoint(id, false) // submit.
}

func (n *MetadataNetwork) delayPublishEndpoint(id uint32, delay bool) {
	n.arbiter.Go(func() {
		if delay {
			select {
			case <-time.After(time.Second * 5):
			case <-n.arbiter.Exit():
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
			oldEndpoints[endpoint.Endpoint] = &endpoint
		}

		var (
			newEndpoints []gossipUtils.NetworkEndpointV1
		)

		localPublish := make([]MetaPeerEndpoint, 0, len(n.backends))
		for endpoint, backend := range n.backends {
			oldPublish, hasOldPublish := oldEndpoints[endpoint]
			if hasOldPublish && oldPublish != nil {
				localPublish = append(localPublish, MetaPeerEndpoint{
					Endpoint: endpoint,
					Priority: backend.Priority(),
					Disabled: oldPublish.Disabled,
				})
				if oldPublish.Disabled {
					continue // do not gossip disabled endpoint.
				}
			} else {
				localPublish = append(localPublish, MetaPeerEndpoint{
					Endpoint: endpoint,
					Priority: backend.Priority(),
					Disabled: false,
				})
			}

			newEndpoints = append(newEndpoints, gossipUtils.NetworkEndpointV1{
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
		}

		// publish locally.
		typePublish := make(map[backend.Type]backend.Backend)
		sort.Slice(localPublish, func(i, j int) bool { return localPublish[i].Higher(localPublish[j]) }) // sort by priority.
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
		pub := MetaPeerStatePublication{}
		pub.Endpoints = localPublish
		n.self.publishInterval(&pub)
	})
}

// UpdateLocalEndpoints updates local network endpoint.
func (n *MetadataNetwork) UpdateLocalEndpoints(creators ...backend.BackendCreator) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.configID++
	id, changed := n.configID, false

	indexCreators := make(map[backend.Endpoint]backend.BackendCreator, len(creators))
	for _, creator := range creators {
		indexCreators[backend.Endpoint{
			Type: creator.Type(),
			// TODO(xutao): support multi-endpoint.
			Endpoint: creator.Publish(),
		}] = creator
	}

	removePublishEndpoint := func(ty backend.Type, except backend.Backend) {
		rb, hasPublish := n.Publish.Type2Backend.Load(ty)
		if !hasPublish {
			return
		}
		b, isBackend := rb.(backend.Backend)
		if !isBackend || except == b {
			n.Publish.Type2Backend.Delete(ty)
		}
	}

	// shutdown.
	for endpoint, backend := range n.backends {
		if backend == nil {
			delete(n.backends, endpoint)
			changed = true
			continue
		}
		if _, hasEndpoint := indexCreators[endpoint]; !hasEndpoint {
			delete(n.backends, endpoint)
			backend.Shutdown()
			removePublishEndpoint(endpoint.Type, backend)
			changed = true
		}
	}

	failCreation := []backend.BackendCreator(nil)
	for endpoint, creator := range indexCreators {
		rb, hasBackend := n.backends[endpoint]
		b, isBackend := rb.(backend.Backend)
		// TODO(xutao): backend support configuration change.
		if hasBackend && isBackend && b != nil { // update
			delete(n.backends, endpoint)
			b.Shutdown()
			removePublishEndpoint(endpoint.Type, b)
			changed = true
		}

		// new.
		new, err := creator.New(n.arbiter, nil)
		if err != nil {
			n.log.Errorf("failed to create backend %v:%v. (err = \"%v\")", creator.Type().String(), creator.Publish, err)
			failCreation = append(failCreation, creator)
		} else {
			n.backends[endpoint] = new
			new.Watch(n.receiveRemote)
		}
	}

	if len(failCreation) > 0 {
		n.delayLocalEndpointCreation(id, failCreation...)
	}

	if changed {
		n.delayPublishEndpoint(id, false)
	}
}
