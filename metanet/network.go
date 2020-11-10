package metanet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/metanet/backend"
	gmodel "github.com/crossmesh/fabric/metanet/gossip"
	"github.com/crossmesh/sladder"
	"github.com/crossmesh/sladder/engine/gossip"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

const (
	lowestEndpointPriority  = uint32(0xFFFE)
	defaultEndpointPriority = lowestEndpointPriority
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

type backendPublish struct {
	backend.Backend
	Priority uint32
}

type managerRuntime struct {
	backend.Manager

	n *MetadataNetwork

	publishEndpoints []string
}

func (r *managerRuntime) UpdatePublish(eps ...string) {
	if r.publishEndpoints != nil {
		r.publishEndpoints = r.publishEndpoints[:0]
	}

	r.publishEndpoints = append(r.publishEndpoints, eps...)

	r.n.RepublishEndpoint()
}

// MetadataNetwork implements simple decentalized communication metadata network.
type MetadataNetwork struct {
	// handlers.
	messageHandlers  sync.Map // map[uint16]MessageHandler
	peerLeaveWatcher sync.Map // map[uintptr]PeerHandler
	peerJoinWatcher  sync.Map // map[uintptr]PeerHandler

	lock sync.RWMutex

	store common.Store

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
		nameResolver *gmodel.PeerNameResolver
		self         *sladder.Node
	}
	self              *MetaPeer
	peers             map[*sladder.Node]*MetaPeer
	nameConflictNodes map[*MetaPeer]struct{}

	// version info.
	versionInfoKey string

	// manage backends.
	backendManagers  map[backend.Type]*managerRuntime
	endpointPriority map[backend.Endpoint]uint32
	epoch            uint32

	// copy-on-write network map publication.
	Publish struct {
		Name2Peer map[string]*MetaPeer // (COW)
		Self      *MetaPeer

		Epoch    uint32
		Backends map[backend.Endpoint]*backendPublish
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
func NewMetadataNetwork(arbiter *arbit.Arbiter, log *logging.Entry, store common.Store) (n *MetadataNetwork, err error) {
	if store == nil {
		return nil, errors.New("nil store")
	}
	n = &MetadataNetwork{
		peers: make(map[*sladder.Node]*MetaPeer),
		//backends:          make(map[backend.Endpoint]backend.Backend),
		backendManagers:   make(map[backend.Type]*managerRuntime),
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
	if err = n.populateStore(store); err != nil {
		return nil, err
	}
	if err = n.initializeMembership(); err != nil {
		return nil, err
	}
	if err = n.initializeVersionInfo(); err != nil {
		return nil, err
	}
	if err = n.reloadEndpointPriorities(); err != nil {
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

// RegisterBackendManager registers backend manager.
func (n *MetadataNetwork) RegisterBackendManager(mgr backend.Manager) error {
	if mgr == nil {
		return nil
	}

	ty := mgr.Type()

	old, _ := n.backendManagers[ty]
	if old != nil {
		err := fmt.Errorf("too many managers with type %v are registered", ty)
		n.log.Error(err)
		return err
	}

	// store.
	store := &common.SubpathStore{Store: n.store}
	store.Prefix = append(store.Prefix, backendStorePath...)
	typeKey := strconv.FormatUint(uint64(ty), 10)
	store.Prefix = append(store.Prefix, typeKey, "store")

	// init
	runtime := &managerRuntime{
		Manager: mgr,
		n:       n,
	}
	res := &managerResourceCollection{
		log:     n.log.WithField("backend", ty),
		arbiter: arbit.NewWithParent(n.arbiters.backend),
		store:   store,
		runtime: runtime,
	}
	if err := mgr.Init(res); err != nil {
		n.log.Errorf("failed to initialize backend manager. [type = %v] (err = \"%v\")", mgr.Type(), err)
		return err
	}
	if err := mgr.Watch(n.receiveRemote); err != nil {
		err = fmt.Errorf("failed to watch message. [type = %v] (err = \"%v\")", mgr.Type(), err)
		n.log.Error(err)
		res.arbiter.Shutdown()
		return err
	}

	n.backendManagers[ty] = runtime

	return nil
}

func (n *MetadataNetwork) populateStore(store common.Store) error {
	if n.store != nil {
		return errors.New("switching store in run time is not supported yet")
	}

	n.store = store

	return nil
}

func (n *MetadataNetwork) reloadEndpointPriorities() error {
	path := []string{"priorities"}
	tx, err := n.store.Txn(true)
	if err != nil {
		n.log.Errorf("cannot start store transaction to load priorities setting. (err = \"%v\")", err)
		return err
	}

	var purgeKeys []string
	var ty backend.Type
	var ep string
	tx.Range(path, func(path []string, data []byte) bool {
		key := path[len(path)-1]
		if len(key) < 3 {
			n.log.Warn("invalid priority setting key \"%q\" found. this key will be purged.")
			purgeKeys = append(purgeKeys, key)
			return true
		}
		rty, err := strconv.ParseUint(key[:2], 16, 8)
		if err != nil {
			n.log.Warn("invalid priority setting key \"%q\" found. this key will be purged. (err = \"%v\")", err)
			purgeKeys = append(purgeKeys, key)
		}
		ty = backend.Type(rty)
		ep = key[2:]
		endpoint := backend.Endpoint{
			Type: ty, Endpoint: ep,
		}
		if len(data) != 4 {
			n.log.Warn("invalid priority setting data for \"%v\". ignored.", endpoint)
			return true
		}

		// load
		priority := binary.BigEndian.Uint32(data)
		n.endpointPriority[endpoint] = priority

		return true
	})

	commit := len(purgeKeys) > 0
	for _, key := range purgeKeys {
		if err := tx.Delete(append(path, key)); err != nil {
			n.log.Warn("cannot remove invalid key \"%q\". skip removal process. (err = \"%v\")", err)
			commit = false
			break
		}
	}

	if commit {
		if err = tx.Commit(); err != nil {
			n.log.Warn("failed to commit transaction. skip cleaning invalid priority key. (err = \"%v\")", err)
			commit = false
		}
	}

	if !commit {
		if err = tx.Rollback(); err != nil {
			n.log.Errorf("failed to rollback transaction. (err = \"%v\")", err)
			return err
		}
	}

	return nil
}

func (n *MetadataNetwork) getEndpointPriority(endpoint backend.Endpoint) uint32 {
	priority, hasPriority := n.endpointPriority[endpoint]
	if hasPriority {
		priority = defaultEndpointPriority
	}
	return priority
}

// SetEndpointPriority sets endpoint priority.
func (n *MetadataNetwork) SetEndpointPriority(endpoint backend.Endpoint, priority uint32) error {
	if priority > lowestEndpointPriority {
		return fmt.Errorf("priority cannot be lower than %v", lowestEndpointPriority)
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	path := []string{"priorities"}

	tx, err := n.store.Txn(true)
	if err != nil {
		n.log.Errorf("cannot start store transaction to set endpoint priority. (err = \"%v\")", err)
		return err
	}
	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	key := fmt.Sprintf("%02x%v", endpoint.Type, endpoint.Endpoint)
	path = append(path, "priorities", key)

	var buf [4]byte

	binary.BigEndian.PutUint32(buf[:], priority)
	if err = tx.Set(path, buf[:]); err != nil {
		n.log.Errorf("failed to write endpoint priority. (err = \"%v\")", err)
		return err
	}

	tx.OnCommit(func() {
		n.endpointPriority[endpoint] = priority
	})
	err = tx.Commit()

	return err
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
	if key == gmodel.DefaultNetworkEndpointKey {
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
