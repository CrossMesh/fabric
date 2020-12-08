package edgerouter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/edgerouter/gossip"
	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/fabric/metanet/backend"
	"github.com/crossmesh/fabric/proto"
	"github.com/crossmesh/netns"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

const (
	defaultGossiperTransportBufferSize = uint(1024)
	defaultRepublishInterval           = time.Second * 5
	defaultAutoIPRefreshInterval       = time.Second * 15
	republishAll                       = uint32(0xFFFFFFFF)
	republishNone                      = uint32(0)
)

// EdgeRouter builds overlay network.
type EdgeRouter struct {
	RepublishInterval time.Duration

	republishC      chan uint32
	pendingAppendC  chan *pendingOverlayMetadataUpdation
	pendingProcessC chan *pendingOverlayMetadataUpdation
	virtualDoC      chan *virtualNetworkDoFibre

	netns           *netns.OperationParameterSet
	metaNet         *metanet.MetadataNetwork
	overlayModelKey string
	overlayModel    *gossip.OverlayNetworksValidatorV1

	log *logging.Entry

	store common.Store

	bufCache *sync.Pool

	lock sync.RWMutex

	driverMessageHandler map[driver.OverlayDriverType]driver.MessageHandler // (COW)

	drivers map[driver.OverlayDriverType]*driverContext

	underlay struct {
		ID int32

		staticIPs common.IPNetSet
		autoIPs   common.IPNetSet

		ids map[*metanet.MetaPeer]int32
		ips map[*metanet.MetaPeer]common.IPNetSet
	}

	networks     map[int32]*networkInfo // active networks.
	idleNetworks map[driver.NetworkID]*networkInfo

	arbiters struct {
		main   *arbit.Arbiter
		driver *arbit.Arbiter
	}
}

// New creates a new EdgeRouter.
func New(arbiter *arbit.Arbiter,
	net *metanet.MetadataNetwork,
	log *logging.Entry,
	store common.Store,
	namespaceBindPath string) (a *EdgeRouter, err error) {
	defer func() {
		if err != nil {
			arbiter.Shutdown()
			arbiter.Join()
		}
	}()
	if net == nil {
		err = errors.New("metanet network is needed")
		return nil, err
	}
	if store == nil {
		err = errors.New("store is needed")
		return nil, err
	}
	if log == nil {
		log = logging.WithField("module", "manager")
	}

	a = &EdgeRouter{
		log:               log,
		metaNet:           net,
		RepublishInterval: defaultRepublishInterval,

		republishC:      make(chan uint32),
		pendingProcessC: make(chan *pendingOverlayMetadataUpdation),
		pendingAppendC:  make(chan *pendingOverlayMetadataUpdation),
		virtualDoC:      make(chan *virtualNetworkDoFibre),

		networks:     map[int32]*networkInfo{},
		idleNetworks: make(map[driver.NetworkID]*networkInfo),
	}
	a.arbiters.main = arbit.NewWithParent(arbiter)
	a.underlay.ids = make(map[*metanet.MetaPeer]int32)
	a.underlay.ips = make(map[*metanet.MetaPeer]common.IPNetSet)
	a.netns = &netns.OperationParameterSet{
		BindMountPath: namespaceBindPath,
	}
	a.bufCache = &sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 8)
		},
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	if err = a.populateStore(store); err != nil {
		return nil, err
	}
	if err = a.startProcessVirtualDo(); err != nil {
		return nil, err
	}
	if err = a.initializeNetworkMap(); err != nil {
		return nil, err
	}

	a.metaNet.RegisterMessageHandler(proto.MsgTypeVNetDriver, a.processDriverMessage)
	//a.metaNet.RegisterMessageHandler(proto.MsgTypeVNetController, a.processVNetControllerMessage)

	a.startRepublishLocalStates()
	a.startProcessPendingUpdates(a.pendingProcessC)
	a.startAcceptPendingUpdates(a.pendingAppendC, a.pendingProcessC)
	a.startCollectUnderlayAutoIPs()

	a.waitCleanUp()
	return a, nil
}

func (r *EdgeRouter) doStoreWriteTxn(proc func(tx common.StoreTxn) (bool, error)) error {
	tx, err := r.store.Txn(true)
	if err != nil {
		r.log.Error("failed to start transaction. (err = \"%v\")", err)
		return err
	}

	var shouldCommit bool

	shouldCommit, err = proc(tx)
	if shouldCommit {
		shouldCommit = err == nil
	}

	if shouldCommit {
		if err = tx.Commit(); err == nil {
			return nil
		}
		r.log.Error("failed to commit transaction. (err = \"%v\")", err)
	}

	if rerr := tx.Rollback(); rerr != nil {
		panic(rerr)
	}

	return nil
}

func (r *EdgeRouter) filterUnderlayIPs(filter func(*net.IPNet) bool) (ips common.IPNetSet) {
	set := r.underlay.autoIPs.Clone()
	set = append(set, r.underlay.staticIPs...)

	for _, ip := range set {
		if !ip.IP.IsGlobalUnicast() {
			continue
		}
		if !filter(ip) {
			continue
		}
		ips = append(ips, ip)
	}
	ips.Build()
	return
}

func (r *EdgeRouter) commitNewUnderlayIPs(set common.IPNetSet) error {
	return r.doStoreWriteTxn(func(tx common.StoreTxn) (bool, error) {
		data, err := common.IPNetSetEncode(set)
		if err != nil {
			err = fmt.Errorf("failed to encode ipnet set. (err = \"%v\")", err)
			return false, err
		}
		path := []string{"static_ips"}
		if err = tx.Set(path, data); err != nil {
			return false, err
		}
		tx.OnCommit(func() { r.underlay.staticIPs = set })
		return true, nil
	})
}

func (r *EdgeRouter) populateStore(store common.Store) error {
	tx, err := store.Txn(true)
	if err != nil {
		r.log.Error("failed to start transaction for store initialization. (err = \"%v\")", err)
		return err
	}

	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	var data []byte

	// load underlay ID.
	path := []string{"underlay_id"}
	if data, err = tx.Get(path); err != nil {
		r.log.Error("failed to load underlay ID. (err = \"%v\")", err)
		return err
	}
	underlayID := int32(-1)
	if len(data) != 4 { // corrupted
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], uint32(underlayID))
		data = buf[:]

		if data != nil {
			// TODO(xutao): may pause service until user reset underlay ID for security reason.
			r.log.Warn("corrupted underlay ID in store. reset to -1.")
		}
		if err = tx.Set(path, data); err != nil {
			r.log.Error("failed to reset underlay ID. (err = \"%v\")", err)
			return err
		}

	} else {
		underlayID = int32(binary.BigEndian.Uint32(data[:]))
	}
	r.underlay.ID = underlayID

	// static IPs.
	path = []string{"static_ips"}
	if data, err = tx.Get(path); err != nil {
		r.log.Error("failed to load static underlay IPs. (err = \"%v\")", err)
		return err
	}
	var ipSet common.IPNetSet
	if data != nil {
		if ipSet, err = common.IPNetSetDecode(data); err != nil {
			r.log.Warn("corrupted static underlay IPs. reseting... (err = \"%v\")", err)

			if data, err = common.IPNetSetEncode(ipSet); err != nil {
				r.log.Error("failed to encode static underlay IPs. (err = \"%v\")", err)
				return err
			}
			if err = tx.Set(path, data); err != nil {
				r.log.Error("failed to reset static underlay IPs. (err = \"%v\")", err)
				return err
			}
		}
		r.underlay.staticIPs = ipSet
	}

	// load networks.
	path = []string{"networks"}

	var purgeNetIDKey []string

	tx.Range(path, func(path []string, data []byte) bool {
		var netID int32

		rawID := path[len(path)-1]
		{
			rid, perr := strconv.ParseInt(rawID, 10, 32)
			if data != nil {
				if perr == nil {
					err = fmt.Errorf("inconsistent network store. %v should not has data", path)
					r.log.Error(err)
					return false
				}
				return true
			}
			netID = int32(rid)
		}

		info := &networkInfo{}
		if data, err = tx.Get(append(path, "driver")); err != nil {
			err = fmt.Errorf("cannot read driver for network %v. (err = \"%v\")", netID, err)
			r.log.Error(err)
			return false
		}
		if data == nil {
			r.log.Warn("driver ID missing for network %v. network %v will be purged.", netID, netID)
			purgeNetIDKey = append(purgeNetIDKey, rawID)
			return true
		}

		var driverType driver.OverlayDriverType
		{
			rid, perr := strconv.ParseUint(string(data), 10, 16)
			if perr != nil {
				r.log.Warn("driver ID currupted for network %v. network %v will be purged.", netID, netID)
				purgeNetIDKey = append(purgeNetIDKey, rawID)
				return true
			}
			driverType = driver.OverlayDriverType(rid)
		}
		info.driverType = driverType

		r.networks[netID] = info
		return true
	})

	for _, key := range purgeNetIDKey {
		if err = tx.Delete(append(path, key)); err != nil {
			r.log.Warn("cannot remove corrupted network %v. (err = \"%v\")", key, err)
		}
	}

	if err = tx.Commit(); err != nil {
		r.log.Errorf("failed to commit transaction. (err = \"%v\")", err)
		return err
	}

	r.store = store

	return nil
}

// ReloadStaticConfig reloads static config.
func (r *EdgeRouter) ReloadStaticConfig(path string) {}

// SeedPeer adds seed endpoint.
func (r *EdgeRouter) SeedPeer(endpoints ...backend.Endpoint) error {
	return r.metaNet.SeedEndpoints(endpoints...)
}

func (r *EdgeRouter) waitCleanUp() {
	r.arbiters.main.Go(func() {
		<-r.arbiters.main.Exit() // watch exit signal.

		r.lock.Lock()
		defer r.lock.Unlock()

		r.arbiters.driver.Shutdown()
		r.arbiters.driver.Join()
		r.log.Info("all overlay drivers shutdown.")
	})
}
