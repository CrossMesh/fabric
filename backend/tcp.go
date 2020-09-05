package backend

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/config"
	"github.com/crossmesh/fabric/proto"
	"github.com/crossmesh/fabric/proto/pb"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

func validTCPPublishEndpoint(arr *net.TCPAddr) bool {
	if arr == nil {
		return false
	}
	return arr.IP.IsGlobalUnicast()
}

const (
	defaultSendTimeout    = 50
	defaultConnectTimeout = 15000
)

// TCPBackendConfig describes TCP backend parameters.
type TCPBackendConfig struct {
	Bind      string `json:"bind" yaml:"bind"`
	Publish   string `json:"publish" yaml:"publish"`
	Priority  uint32 `json:"priority" yaml:"priority"`
	StartCode string `json:"startCode" yaml:"startCode"`

	SendTimeout     uint32 `json:"sendTimeout" yaml:"sendTimeout" default:"50"`
	SendBufferSize  int    `json:"sendBuffer" yaml:"sendBuffer" default:"0"`
	KeepalivePeriod int    `json:"keepalivePeriod" yaml:"keepalivePeriod" default:"60"`
	ConnectTimeout  uint32 `json:"connectTimeout" yaml:"connectTimeout" default:"15"`

	// drain options
	EnableDrainer        bool   `json:"enableDrainer" yaml:"enableDrainer" default:"false"`
	MaxDrainBuffer       uint32 `json:"maxDrainBuffer" yaml:"maxDrainBuffer" default:"8388608"`          // maximum drain buffer in byte.
	MaxDrainLatancy      uint32 `json:"maxDrainLatency" yaml:"maxDrainLatency" default:"500"`            // maximum latency tolerance in microsecond.
	DrainStatisticWindow uint32 `json:"drainStatisticWindow" yaml:"drainStatisticWindow" default:"1000"` // statistic window in millisecond
	BulkThreshold        uint32 `json:"bulkThreshold" yaml:"bulkTHreshold" default:"2097152"`            // rate threshold (Bps) to trigger bulk mode.

	raw *config.Backend
}

type tcpCreator struct {
	cfg TCPBackendConfig
}

func newTCPCreator(cfg *config.Backend) (BackendCreator, error) {
	c := &tcpCreator{}
	if cfg.Parameters == nil {
		return nil, ErrInvalidBackendConfig
	}
	// re-parse
	bin, err := json.Marshal(cfg.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parse backend config failure (%v)", err)
	}
	if err = json.Unmarshal(bin, &c.cfg); err != nil {
		return nil, fmt.Errorf("parse backend config failure (%v)", err)
	}
	c.cfg.raw = cfg
	return c, nil
}

func (c *tcpCreator) Type() pb.PeerBackend_BackendType { return pb.PeerBackend_TCP }
func (c *tcpCreator) Priority() uint32                 { return c.cfg.Priority }
func (c *tcpCreator) Publish() string                  { return c.cfg.Publish }
func (c *tcpCreator) New(arbiter *arbit.Arbiter, log *logging.Entry) (Backend, error) {
	return NewTCP(arbiter, log, &c.cfg, &c.cfg.raw.PSK)
}

// TCP implements TCP backend.
type TCP struct {
	bind     *net.TCPAddr
	listener *net.TCPListener

	config *TCPBackendConfig
	psk    *string

	log *logging.Entry

	link         sync.Map
	resolveCache sync.Map

	watch  sync.Map
	connID uint32

	Arbiter *arbit.Arbiter
}

var (
	ErrNonTCPConnection = errors.New("got non-tcp connection")
	ErrTCPAmbigousRole  = errors.New("ambigous tcp role")
)

// NewTCP creates TCP backend.
func NewTCP(arbiter *arbit.Arbiter, log *logging.Entry, cfg *TCPBackendConfig, psk *string) (t *TCP, err error) {
	if log == nil {
		log = logging.WithField("module", "backend_tcp")
	}
	t = &TCP{
		psk:    psk,
		config: cfg,
		log:    log,
	}
	if cfg.Publish == "" {
		cfg.Publish = cfg.Bind
	}
	t.Arbiter = arbit.NewWithParent(arbiter)
	t.Arbiter.Go(func() {
		var err error

		for t.Arbiter.ShouldRun() {
			if err != nil {
				log.Info("retry in 3 second.")
				time.Sleep(time.Second * 3)
			}
			err = nil

			if t.bind, err = net.ResolveTCPAddr("tcp", cfg.Bind); err != nil {
				log.Error("resolve bind address failure: ", err)
				continue
			}

			t.serve()
		}
	})

	return t, nil
}

// Priority returns priority of backend.
func (t *TCP) Priority() uint32 {
	return t.config.Priority
}

func getDefaultUint32(ori, def uint32) uint32 {
	if ori > 0 {
		return ori
	}
	return def
}

func (t *TCP) getSendTimeout() time.Duration {
	return time.Duration(getDefaultUint32(t.config.SendTimeout, defaultSendTimeout)) * time.Millisecond
}

func (t *TCP) getConnectTimeout() time.Duration {
	return time.Duration(getDefaultUint32(t.config.ConnectTimeout, defaultConnectTimeout)) * time.Millisecond
}

func (t *TCP) getRoutinesCount() (n uint) {
	return t.config.raw.GetMaxConcurrency()
}

func (t *TCP) serve() (err error) {
	for t.Arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if t.listener, err = net.ListenTCP("tcp", t.bind); err != nil {
			t.log.Errorf("cannot listen to \"%v\": %v", t.bind.String(), err)
			continue
		}
		t.log.Infof("listening to %v", t.bind.String())

		err = t.acceptConnection()
		t.listener.Close()
		t.listener = nil
	}
	return
}

func (t *TCP) acceptConnection() (err error) {
	var conn net.Conn

	t.log.Debugf("start accepting connection.")

	for t.Arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = t.listener.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			t.log.Error("set deadline error: ", err)
			continue
		}

		if conn, err = t.listener.Accept(); err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
				continue
			}
			t.log.Error("listener.Accept() error: ", err)
			continue
		}
		connID := atomic.AddUint32(&t.connID, 1)

		t.goServeConnection(connID, conn)
		conn = nil
	}

	t.log.Debugf("stop accepting connection.")

	return nil
}

func (t *TCP) goServeConnection(connID uint32, conn net.Conn) {
	log := t.log.WithField("conn_id", connID)
	log.Infof("incomming connection %v from %v", connID, conn.RemoteAddr())

	t.Arbiter.Go(func() {
		accepted, err := t.handshakeConnect(log, connID, conn)
		if err != nil {
			log.Errorf("handshake error for connection %v: %v", connID, err)
			return
		}
		if !accepted {
			log.Info("connection deined.")
			conn.Close()
		}
	})

}

func (t *TCP) getStartCode(log *logging.Entry) (lead []byte) {
	if t.config.StartCode == "" {
		return
	}
	lead = make([]byte, hex.DecodedLen(len(t.config.StartCode)))
	_, err := hex.Decode(lead, []byte(t.config.StartCode))
	if err != nil {
		log.Warnf("invalid startcode config: %v. (parse get error \"%v\")", t.config.StartCode, err)
		return nil
	}
	return
}

func (t *TCP) acceptTCPLink(log *logging.Entry, link *TCPLink, connectArg *proto.Connect) (bool, error) {
	// protocol version
	switch connectArg.Version {
	case proto.ConnectNoCrypt:
		link.InitializeNoCryption()

	case proto.ConnectAES256GCM:
		// let it is.
	default:
		log.Errorf("invalid connecting protocol version %v.", connectArg.Version)
		return false, nil
	}
	log.Debugf("protocol version: %v", connectArg.Version)

	addr, isTCPAddr := link.conn.RemoteAddr().(*net.TCPAddr)
	if !isTCPAddr {
		log.Warnf("got non-tcp address. closing...")
	}
	key := connectArg.Identity
	leftLink := t.getLink(key)
	link.publish = key
	link.remote = addr
	if !leftLink.assign(link) {
		log.Warnf("link to foreign peer \"%v\" exists. closing...", key)
		return false, nil
	}
	link = leftLink

	t.goTCPLinkDaemon(log, key, link)

	return true, nil
}

func (t *TCP) getLink(key string) (link *TCPLink) {
	init := func() {
		link = newTCPLink(t)
		link.publish = key
	}
	v, loaded := t.link.Load(key)
	for {
		if loaded && v != nil {
			link, loaded = v.(*TCPLink)
			if loaded && link != nil {
				return
			}
			// Defensive code
			init()
			t.link.Store(key, link)
		} else {
			init()
		}
		if v, loaded = t.link.LoadOrStore(key, link); loaded {
			continue
		}
		break
	}
	return
}

// Port retuens local bind port of tcp backend.
func (t *TCP) Port() uint16 {
	return uint16(t.bind.Port)
}

// Type returns backend type ID.
func (t *TCP) Type() pb.PeerBackend_BackendType {
	return pb.PeerBackend_TCP
}

// Publish returns publish endpoint.
func (t *TCP) Publish() (id string) {
	return t.config.Publish
}

// Shutdown closes backend.
func (t *TCP) Shutdown() {
	t.Arbiter.Shutdown()
	t.Arbiter.Join()
}

func (t *TCP) resolve(endpoint string) (addr *net.TCPAddr, err error) {
	v, ok := t.resolveCache.Load(endpoint)
	if !ok || v == nil {
		if addr, err = net.ResolveTCPAddr("tcp", endpoint); err != nil {
			t.log.Errorf("destination \"%v\" not resolved: %v", v, err)
			return nil, err
		}
		t.resolveCache.Store(endpoint, addr)
	} else {
		addr = v.(*net.TCPAddr)
	}
	return
}

// Watch registers callback to receive packet.
func (t *TCP) Watch(proc func(Backend, []byte, string)) error {
	if proc != nil {
		t.watch.Store(&proc, proc)
	}
	return nil
}

// IP returns bind IP.
func (t *TCP) IP() net.IP {
	return t.bind.IP
}
