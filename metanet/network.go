package metanet

import (
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
	backends map[backend.Endpoint]backend.Backend
	epoch    uint32

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
	probeCounter    uint64
	probeLock       sync.RWMutex
	probes          map[linkPathKey]*endpointProbingContext
	recentSuccesses map[linkPathKey]*lastProbingContext
}

// NewMetadataNetwork creates a metadata network.
func NewMetadataNetwork(arbiter *arbit.Arbiter, log *logging.Entry) (n *MetadataNetwork, err error) {
	n = &MetadataNetwork{
		peers:             make(map[*sladder.Node]*MetaPeer),
		backends:          make(map[backend.Endpoint]backend.Backend),
		quitChan:          make(chan struct{}),
		nameConflictNodes: map[*MetaPeer]struct{}{},

		ProbeBrust:      defaultHealthyCheckProbeBrust,
		ProbeTimeout:    defaultHealthyCheckProbeTimeout,
		probes:          make(map[linkPathKey]*endpointProbingContext),
		recentSuccesses: make(map[linkPathKey]*lastProbingContext),
	}

	n.arbiters.main = arbit.NewWithParent(arbiter)
	n.arbiters.backend = arbit.NewWithParent(nil)

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

	n.lock.Lock()
	defer n.lock.Unlock()

	n.gossip.cluster.Quit()
	n.log.Debug("gossip stopped.")

	// stop backends.
	n.arbiters.backend.Shutdown()
	n.arbiters.backend.Join()

	for endpoint, backend := range n.backends {
		backend.Shutdown()
		n.log.Infof("endpoint %v stopped.", endpoint)
		delete(n.backends, endpoint)
	}
	n.Publish.Backends = nil

	close(n.quitChan)
}

func (n *MetadataNetwork) delayLocalEndpointCreation(epoch uint32, creators ...backend.BackendCreator) {
	n.arbiters.main.Go(func() {
		select {
		case <-time.After(time.Second * 5):
		case <-n.arbiters.main.Exit():
			return
		}

		n.lock.Lock()
		defer n.lock.Unlock()

		if epoch < n.Publish.Epoch {
			return
		}

		failCreation, changed := []backend.BackendCreator(nil), false
		for _, creator := range creators {
			endpoint := backend.Endpoint{
				Type:     creator.Type(),
				Endpoint: creator.Publish(),
			}

			// new.
			new, err := creator.New(n.arbiters.backend, nil)
			if err != nil {
				n.log.Errorf("failed to create backend %v:%v. (err = \"%v\")", creator.Type().String(), creator.Publish, err)
				failCreation = append(failCreation, creator)
			} else {
				n.backends[endpoint] = new
				new.Watch(n.receiveRemote)
				changed = true
			}
		}

		if changed {
			n.delayPublishEndpoint(epoch, false)
		}

		if len(failCreation) > 0 {
			epoch++
			n.delayLocalEndpointCreation(epoch, failCreation...)
		}

		if epoch > n.epoch {
			n.epoch = epoch
		}
	})
}

// UpdateLocalEndpoints updates local network endpoint.
func (n *MetadataNetwork) UpdateLocalEndpoints(creators ...backend.BackendCreator) {
	n.lock.Lock()
	defer n.lock.Unlock()

	changed := false

	indexCreators := make(map[backend.Endpoint]backend.BackendCreator, len(creators))
	for _, creator := range creators {
		indexCreators[backend.Endpoint{
			Type: creator.Type(),
			// TODO(xutao): support multi-endpoint.
			Endpoint: creator.Publish(),
		}] = creator
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
			n.log.Infof("endpoint %v stopped.", endpoint)
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
			n.log.Infof("try to restart endpoint %v.", endpoint)
			b.Shutdown()
			changed = true
		}

		// new.
		new, err := creator.New(n.arbiters.backend, nil)
		if err != nil {
			n.log.Errorf("failed to create backend %v:%v. (err = \"%v\")", creator.Type().String(), creator.Publish, err)
			failCreation = append(failCreation, creator)
		} else {
			changed = true
			n.log.Infof("endpoint %v started.", endpoint)
			n.backends[endpoint] = new
			new.Watch(n.receiveRemote)
		}
	}

	epoch := n.epoch + 1
	if changed {
		n.delayPublishEndpoint(epoch, false)
		epoch++
	}

	if len(failCreation) > 0 {
		n.delayLocalEndpointCreation(n.epoch, failCreation...)
	}

	if epoch > n.epoch {
		n.epoch = epoch
	}
}
