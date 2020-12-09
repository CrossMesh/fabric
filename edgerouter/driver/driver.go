package driver

import (
	"math"
	"strconv"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/metanet"
	arbit "github.com/sunmxt/arbiter"
)

const (
	// NetIDUnderlay represents underlay network.
	NetIDUnderlay = math.MinInt32
)

// NetworkID is overlay network identifier.
type NetworkID struct {
	ID         int32             `json:"id,omitempty"`
	DriverType OverlayDriverType `json:"drv,omitempty"`
}

func (id NetworkID) String() string {
	return id.DriverType.String() + "/" + strconv.FormatInt(int64(id.ID), 10)
}

// OverlayDriverType is global ovaley network driver ID.
type OverlayDriverType uint16

func (t OverlayDriverType) String() string {
	switch t {
	case MetanetOverlayDriver:
		return "metanet"
	case VxLAN:
		return "vxlan"
	}
	return "unknown"
}

const (
	UnknownOverlayDriver = OverlayDriverType(0)

	MetanetOverlayDriver = OverlayDriverType(1)
	VxLAN                = OverlayDriverType(3)
)

// UnsupportedError raised when driver is unable to implements some network features.
type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string { return e.Reason }

type Event uint8

// PeerEventHandler handles events about peers.
type PeerEventHandler func(*metanet.MetaPeer, uint8) bool

// RemoteOptionMapWatcher handles remote option map of peers.
type RemoteOptionMapWatcher func(*metanet.MetaPeer, map[string][]byte) bool

// UnderlayIDWatcher handles changes of peer's underlay ID.
type UnderlayIDWatcher func(*metanet.MetaPeer, int32) bool

// UnderlayIPWatcher handles changes of peer's underlay IPs.
type UnderlayIPWatcher func(peer *metanet.MetaPeer, public, private common.IPNetSet) bool

var (
	PeerLeft = uint8(1)
	PeerJoin = uint8(2)
)

// OverlayNetworkMap describes metadata for single virtual network plane.
type OverlayNetworkMap interface {
	// local options.
	GetLocalOption(key string) []byte
	SetLocalOption(key string, data []byte) []byte

	// remote options.
	RemoteOptions(*metanet.MetaPeer) map[string][]byte
	WatchRemoteOptions(RemoteOptionMapWatcher)

	// peers
	Peers() []*metanet.MetaPeer
	WatchMemebershipChanged(PeerEventHandler)

	// peers' underlay ID.
	UnderlayID(*metanet.MetaPeer) int32
	WatchUnderlayID(UnderlayIDWatcher)

	// peers' underlay IPs.
	PeerIPs(*metanet.MetaPeer) (public, private common.IPNetSet)
	WatchPeerIPs(UnderlayIPWatcher)
}

// Logger accepts logs from driver.
type Logger interface {
	Fatal(v ...interface{})
	Fatalf(fotmat string, v ...interface{})

	Error(v ...interface{})
	Errorf(fotmat string, v ...interface{})

	Warn(v ...interface{})
	Warnf(fotmat string, v ...interface{})

	Info(v ...interface{})
	Infof(fotmat string, v ...interface{})

	Debug(v ...interface{})
	Debugf(fotmat string, v ...interface{})
}

// ResourceCollection contains exported resources provided for OverlayDriver.
type ResourceCollection interface {
	// persistent key-value store.
	Store() common.Store

	// goroutine lifecycle manager.
	Arbiter() *arbit.Arbiter

	NetworkMap(netID int32) OverlayNetworkMap
	Messager() Messager
	Logger() Logger

	// Run code inside virtualized network environment.
	VirtualDo(int32, func(underlayID, overlayID int32) error) error
}

// MessageHandler accepts incoming message.
type MessageHandler func(peer *metanet.MetaPeer, msg []byte) bool

// Messager implements communicating methods for driver among peers.
type Messager interface {
	Send(peer *metanet.MetaPeer, msg []byte)
	// ReliableSend(msg []byte)

	// register new message handler and returns existing one.
	WatchMessage(MessageHandler) MessageHandler
}

// OverlayDriver provides overlay network supporting.
type OverlayDriver interface {
	Type() OverlayDriverType

	Init(ResourceCollection) error

	AddLink(name string, netID int32) error
	DelLink(name string, netID int32) error
}
