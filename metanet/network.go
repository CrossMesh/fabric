package metanet

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/crossmesh/fabric/backend"
	gossipUtils "github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/sladder"
	"github.com/crossmesh/sladder/engine/gossip"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

// MessageBrokenError indicates message is broken.
type MessageBrokenError struct {
	reason string
}

func (e *MessageBrokenError) Error() (s string) {
	s = "message broken"
	if e.reason != "" {
		s += ": " + e.reason
	}
	return
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
	// handlers.
	messageHandlers  sync.Map // map[uint16]MessageHandler
	peerLeaveWatcher sync.Map // map[uintptr]PeerHandler
	peerJoinWatcher  sync.Map // map[uintptr]PeerHandler

	lock sync.RWMutex

	store Store

	// goroutine arbiting.
	arbiters struct {
		main    *arbit.Arbiter
		backend *arbit.Arbiter
	}

	// logging.
	log *logging.Entry

	quitChan chan struct{}

	// gossip fields.
	gossip struct {
		cluster      *sladder.Cluster
		engine       *gossip.EngineInstance
		transport    *gossipEngineTransport
		nameResolver *gossipUtils.PeerNameResolver
		self         *sladder.Node
	}
	self              *MetaPeer
	peers             map[*sladder.Node]*MetaPeer
	nameConflictNodes map[*MetaPeer]struct{}

	// version info.
	versionInfoKey string

	// manage backends.
	backendManagers map[backend.Type]BackendManager
	//backends        map[backend.Endpoint]backend.Backend
	epoch uint32

	// copy-on-write network map publication.
	Publish struct {
		Name2Peer map[string]*MetaPeer // (COW)
		Self      *MetaPeer

		Epoch    uint32
		Backends map[backend.Endpoint]backend.Backend
	}

	// health checking parameters.
	ProbeBrust   int
	ProbeTimeout time.Duration

	// health checking fields.
	lastFails       sync.Map // map[linkPathKey]*MetaPeer
	probeLock       sync.RWMutex
	probeCounter    uint64
	probes          map[linkPathKey]*endpointProbingContext
	recentSuccesses map[linkPathKey]*lastProbingContext
}

// NewMetadataNetwork creates a metadata network.
func NewMetadataNetwork(arbiter *arbit.Arbiter, log *logging.Entry, store Store) (n *MetadataNetwork, err error) {
	if store == nil {
		return nil, errors.New("nil store")
	}
	n = &MetadataNetwork{
		peers: make(map[*sladder.Node]*MetaPeer),
		//backends:          make(map[backend.Endpoint]backend.Backend),
		backendManagers:   make(map[backend.Type]BackendManager),
		quitChan:          make(chan struct{}),
		nameConflictNodes: map[*MetaPeer]struct{}{},

		ProbeBrust:      defaultHealthyCheckProbeBrust,
		ProbeTimeout:    defaultHealthyCheckProbeTimeout,
		probes:          make(map[linkPathKey]*endpointProbingContext),
		recentSuccesses: make(map[linkPathKey]*lastProbingContext),
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	n.arbiters.main = arbit.NewWithParent(arbiter)
	n.arbiters.backend = arbit.NewWithParent(nil)

	defer func() {
		if err != nil {
			n.arbiters.main.Shutdown()
			n.arbiters.backend.Shutdown()
			n.arbiters.main.Join()
			n.arbiters.backend.Join()
		}
	}()

	if log == nil {
		n.log = logging.WithField("module", "metadata_network")
	} else {
		n.log = log
	}
	if err = n.initializeMembership(); err != nil {
		return nil, err
	}
	if err = n.initializeVersionInfo(); err != nil {
		return nil, err
	}
	if err = n.populateStore(store); err != nil {
		return nil, err
	}

	n.arbiters.main.Go(func() {
		select {
		case <-n.arbiters.main.Exit():
			n.Close()
		case <-n.quitChan:
		}
	})

	n.initializeEndpointHealthCheck()

	return n, nil
}

func (n *MetadataNetwork) populateStore(store Store) error {
	if n.store != nil {
		return errors.New("switching store in run time is not supported yet")
	}

	n.store = store

	// activate endpoints.
	n.epoch++
	n.delayReactivateEndpoints(n.epoch, 0)

	return nil
}

// SetRegion sets region of self.
func (n *MetadataNetwork) SetRegion(region string) (string, error) {
	engine := n.gossip.engine
	if engine == nil {
		return "", nil
	}
	return engine.SetRegion(region)
}

// SetMinRegionPeer sets the minimum number of peers in region.
func (n *MetadataNetwork) SetMinRegionPeer(p uint) uint {
	engine := n.gossip.engine
	if engine == nil {
		return 0
	}
	return engine.SetMinRegionPeer(p)
}

// SetGossipQuitTimeout sets QuitTimeout of gossip engine.
func (n *MetadataNetwork) SetGossipQuitTimeout(d time.Duration) {
	engine := n.gossip.engine
	if engine == nil {
		return
	}
	engine.QuitTimeout = d
	return
}

// GetGossipQuitTimeout reports current QuitTimeout of gossip engine.
func (n *MetadataNetwork) GetGossipQuitTimeout() time.Duration {
	engine := n.gossip.engine
	if engine == nil {
		return 0
	}
	return engine.QuitTimeout
}

// RegisterDataModel registers new data model to sladder framework.
func (n *MetadataNetwork) RegisterDataModel(
	key string,
	validator sladder.KVValidator,
	versionWrap bool, forceReplace bool,
	flags uint32) error {
	if key == gossipUtils.DefaultNetworkEndpointKey {
		return &KeyReservedError{key: key}
	}
	if versionWrap {
		validator = n.gossip.engine.WrapVersionKVValidator(validator)
	}
	return n.gossip.cluster.RegisterKey(key, validator, forceReplace, flags)
}

// SladderTxn executes transaction on Sladder framework.
func (n *MetadataNetwork) SladderTxn(do func(t *sladder.Transaction) bool) error {
	return n.gossip.cluster.Txn(do)
}

// KeyChangeWatcher accepts key change event.
type KeyChangeWatcher func(peer *MetaPeer, meta sladder.KeyValueEventMetadata) bool

// WatchKeyChanges registers key changes watcher.
func (n *MetadataNetwork) WatchKeyChanges(watcher KeyChangeWatcher, keys ...string) bool {
	if watcher == nil {
		return false
	}
	ctx := n.gossip.cluster.Keys(keys...).Watch(func(ctx *sladder.WatchEventContext, meta sladder.KeyValueEventMetadata) {
		n.lock.RLock()
		peer, hasPeer := n.peers[meta.Node()]
		n.lock.RUnlock()
		if !hasPeer {
			return
		}

		if !watcher(peer, meta) {
			ctx.Unregister()
		}
	})
	return ctx != nil
}

// Close quits from metadata network.
func (n *MetadataNetwork) Close() {
	n.log.Debug("metanet closing...")

	n.gossip.cluster.Quit()
	n.log.Debug("gossip stopped.")

	// stop backends.
	n.arbiters.backend.Shutdown()
	n.arbiters.backend.Join()

	n.lock.Lock()

	n.Publish.Backends = nil

	close(n.quitChan)

	n.lock.Unlock()
}

func (n *MetadataNetwork) delayReactivateEndpoints(epoch uint32, d time.Duration) {
	n.arbiters.main.Go(func() {
		if d > 0 {
			select {
			case <-time.After(d):
			case <-n.arbiters.main.Exit():
				return
			}
		}

		n.lock.Lock()
		defer n.lock.Unlock()

		if epoch < n.Publish.Epoch {
			return
		}

		tx, err := n.store.Txn(false)
		if err != nil {
			n.log.Error("endpoint reactivation got transaction failure. retry later. (err = \"%v\")", err)
			n.delayReactivateEndpoints(epoch, time.Second*5)
			return
		}

		var basePath []string
		basePath = append(basePath, backendStorePath...)

		changed, failure := false, false

		for ty, mgr := range n.backendManagers {
			typeKey := strconv.FormatUint(uint64(ty), 10)
			path := append(basePath, typeKey, "active")
			v, err := tx.Get(path)
			if err != nil {
				n.log.Error("endpoint reactivation got txn.Get() failure. retry later. (err = \"%v\")", err)
				n.delayReactivateEndpoints(epoch, time.Second*5)
				return
			}

			expected := &storedActiveEndpoints{}
			if err = json.Unmarshal(v, expected); err != nil {
				n.log.Warn("cannot decode metadata of active endpoint. store may be corrupted. treat it as no active endpoint. (err = \"%v\")", err)
			}

			activeEndpointSet := make(map[string]struct{}, len(expected.endpoints))
			for _, ep := range expected.endpoints {
				activeEndpointSet[ep] = struct{}{}
			}

			for _, endpoint := range mgr.ListActiveEndpoints() {
				_, active := activeEndpointSet[endpoint]
				if !active {
					if err = mgr.Deactivate(endpoint); err != nil {
						n.log.Warn("cannot deactivate %v. retry later. (err = \"%v\")", backend.Endpoint{
							Type: ty, Endpoint: endpoint,
						})
						failure = true
					}
					changed = true
				}
			}

			for ep := range activeEndpointSet {
				if mgr.GetBackend(ep) == nil {
					if err = mgr.Activate(ep); err != nil {
						n.log.Warn("cannot activate %v. retry later. (err = \"%v\")", backend.Endpoint{
							Type: ty, Endpoint: ep,
						})
						failure = true
					}
					changed = true
				}
			}
		}

		if changed {
			n.delayPublishEndpoint(epoch, false)
		}

		if failure {
			epoch++
			n.delayReactivateEndpoints(epoch, d)
		}

		if epoch > n.epoch {
			n.epoch = epoch
		}
	})
}

func (n *MetadataNetwork) ensureEndpointActiveState(activated bool, endpoints ...backend.Endpoint) error {
	if len(endpoints) < 1 {
		return nil
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	tx, err := n.store.Txn(true)
	if err != nil {
		return err
	}

	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].Type < endpoints[j].Type })

	changed := false
	defer func() {
		if err != nil || !changed {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	var basePath []string
	basePath = append(basePath, backendStorePath...)

	for cur := 0; cur < len(endpoints); {
		ty := endpoints[0].Type
		activeSet := make(map[string]struct{})
		needWriteBack := false

		// read current states.
		typeKey := strconv.FormatUint(uint64(ty), 10)
		path := append(basePath, typeKey, "active")
		v, err := tx.Get(path)
		if err != nil {
			return err
		}
		stored := &storedActiveEndpoints{}
		if err = json.Unmarshal(v, stored); err != nil {
			n.log.Warn("cannot decode metadata of active endpoints. metadata may be corrupted. treat it as no active endpoint. (err = \"%v\")", err)
		}
		activeSet = make(map[string]struct{}, len(stored.endpoints))
		for _, endpoint := range stored.endpoints {
			activeSet[endpoint] = struct{}{}
		}

		// modify
		for endpoint := endpoints[cur]; endpoint.Type == ty && cur < len(endpoints); cur++ {
			_, curActivated := activeSet[endpoint.Endpoint]
			if curActivated == activated {
				continue
			}
			needWriteBack = true
			if activated {
				activeSet[endpoint.Endpoint] = struct{}{}
			} else {
				delete(activeSet, endpoint.Endpoint)
			}
		}

		// store.
		if needWriteBack {
			stored := &storedActiveEndpoints{}
			for endpoint := range activeSet {
				stored.endpoints = append(stored.endpoints, endpoint)
			}
			v, err := json.Marshal(stored)
			if err != nil {
				n.log.Error("failed to store metadata of active endpoints. (err = \"%v\")", err)
				return err
			}
			if err = tx.Set(path, v); err != nil {
				n.log.Error("failed to store encoded metadata. (err = \"%v\")", err)
				return err
			}

			changed = true
		}
	}

	if changed {
		epoch := n.epoch + 1
		tx.OnCommit(func() {
			n.delayReactivateEndpoints(epoch, 0)
			n.epoch = epoch
		})
	}

	return nil
}

// DeactivateEndpoint deactivates endpoints.
func (n *MetadataNetwork) DeactivateEndpoint(endpoints ...backend.Endpoint) error {
	return n.ensureEndpointActiveState(false, endpoints...)
}

// ActivateEndpoint activates endpoints.
func (n *MetadataNetwork) ActivateEndpoint(endpoints ...backend.Endpoint) error {
	return n.ensureEndpointActiveState(true, endpoints...)
}
