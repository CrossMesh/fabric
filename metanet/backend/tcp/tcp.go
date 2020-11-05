package tcp

import (
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/metanet/backend"
	"github.com/crossmesh/fabric/proto"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

const (
	defaultBufferSize = 4096
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

var (
	ErrNonTCPConnection = errors.New("got non-tcp connection")
	ErrTCPAmbigousRole  = errors.New("ambigous tcp role")
)

type tcpEndpoint struct {
	shouldActive bool

	endpoint   string
	parameters parameters

	lock    sync.Mutex
	Arbiter *arbit.Arbiter

	bind     *net.TCPAddr
	listener *net.TCPListener

	log *logging.Entry

	link         sync.Map
	resolveCache sync.Map

	watch  sync.Map
	connID uint32
}

func newTCPEndpoint() *tcpEndpoint {
	endpoint := &tcpEndpoint{}
	endpoint.parameters.ResetDefault()
	return endpoint
}

func (e *tcpEndpoint) Active() bool {
	return e.listener != nil
}

func (e *tcpEndpoint) Activate(arbiter *arbit.Arbiter, log *logging.Entry) error {
	if e.listener != nil {
		return nil
	}
	if log == nil {
		log = logging.WithField("module", "endpoint_tcp")
	}
	e.lock.Lock()
	defer e.lock.Unlock()

	e.shouldActive = true

	e.Arbiter = arbit.NewWithParent(arbiter)
	e.log = log
	e.Arbiter.Go(func() {
		var err error

		for e.Arbiter.ShouldRun() {
			if err != nil {
				log.Info("retry in 3 second.")
				time.Sleep(time.Second * 3)
			}
			err = nil

			if e.bind, err = net.ResolveTCPAddr("tcp", e.parameters.Bind); err != nil {
				log.Error("resolve bind address failure: ", err)
				continue
			}

			e.serve()
		}

		// cleaning.
		e.lock.Lock()
		defer e.lock.Unlock()

		e.link.Range(func(k, v interface{}) bool {
			e.link.Delete(k)
			return true
		})
		e.resolveCache.Range(func(k, v interface{}) bool {
			e.resolveCache.Delete(k)
			return true
		})
		e.watch.Range(func(k, v interface{}) bool {
			e.watch.Delete(k)
			return true
		})
	})
	return nil
}

func (e *tcpEndpoint) Deactivate() {
	e.shouldActive = false

	arbiter := e.Arbiter
	if arbiter != nil {
		arbiter.Shutdown()
		arbiter.Join()
	}
	return
}

func (e *tcpEndpoint) serve() (err error) {
	for e.Arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		e.lock.Lock()
		if e.listener, err = net.ListenTCP("tcp", e.bind); err != nil {
			e.log.Errorf("cannot listen to \"%v\": %v", e.bind.String(), err)
			continue
		}
		e.log.Infof("listening to %v", e.bind.String())
		e.lock.Unlock()

		err = e.acceptConnection()

		e.lock.Lock()
		e.listener.Close()
		e.listener = nil
		e.lock.Unlock()
	}
	return
}

func (e *tcpEndpoint) acceptConnection() (err error) {
	var conn net.Conn

	e.log.Debugf("start accepting connection.")

	for e.Arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = e.listener.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			e.log.Error("set deadline error: ", err)
			continue
		}

		if conn, err = e.listener.Accept(); err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
				continue
			}
			e.log.Error("listener.Accept() error: ", err)
			continue
		}
		connID := atomic.AddUint32(&e.connID, 1)

		e.goServeConnection(connID, conn)
		conn = nil
	}

	e.log.Debugf("stop accepting connection.")

	return nil
}

func (e *tcpEndpoint) goServeConnection(connID uint32, conn net.Conn) {
	log := e.log.WithField("conn_id", connID)
	log.Infof("incomming connection %v from %v", connID, conn.RemoteAddr())

	e.Arbiter.Go(func() {
		accepted, err := e.handshakeConnect(log, connID, conn)
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

func getDefaultUint32(ori, def uint32) uint32 {
	if ori > 0 {
		return ori
	}
	return def
}

func (e *tcpEndpoint) getSendTimeout() time.Duration {
	return time.Duration(getDefaultUint32(e.parameters.SendTimeout, defaultSendTimeout)) * time.Millisecond
}

func (e *tcpEndpoint) getConnectTimeout() time.Duration {
	return time.Duration(getDefaultUint32(e.parameters.ConnectTimeout, defaultConnectTimeout)) * time.Millisecond
}

func (e *tcpEndpoint) getRoutinesCount() (n uint) {
	return e.parameters.MaxConcurrency
}

func (e *tcpEndpoint) getStartCode(log *logging.Entry) (lead []byte) {
	if e.parameters.StartCode == "" {
		return
	}
	lead = make([]byte, hex.DecodedLen(len(e.parameters.StartCode)))
	if _, err := hex.Decode(lead, []byte(e.parameters.StartCode)); err != nil {
		log.Warnf("invalid startcode config: %v. (parse get error \"%v\")", e.parameters.StartCode, err)
		return nil
	}
	return
}

func (e *tcpEndpoint) acceptTCPLink(log *logging.Entry, link *TCPLink, connectArg *proto.Connect) (bool, error) {
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
	leftLink := e.getLink(key)
	link.publish = key
	link.remote = addr

	// TODO(xutao): may force to replace previous link. because network partition may
	// make connection in one side closed and the other side definitely won't notice that for a short period.
	// Under the circumstance, link won't recover until the other side notices a broken TCP connection and closes it.
	// We may directly close previous connection immediately.
	if !leftLink.assign(link) {
		log.Warnf("link to foreign peer \"%v\" exists. closing...", key)
		return false, nil
	}
	link = leftLink

	e.goTCPLinkDaemon(log, key, link)

	return true, nil
}

func (e *tcpEndpoint) getLink(key string) (link *TCPLink) {
	init := func() {
		link = newTCPLink(e)
		link.publish = key
	}
	v, loaded := e.link.Load(key)
	for {
		if loaded && v != nil {
			link, loaded = v.(*TCPLink)
			if loaded && link != nil {
				return
			}
			// Defensive code
			init()
			e.link.Store(key, link)
		} else {
			init()
		}
		if v, loaded = e.link.LoadOrStore(key, link); loaded {
			continue
		}
		break
	}
	return
}

// Port retuens local bind port of tcp backend.
func (e *tcpEndpoint) Port() uint16 {
	return uint16(e.bind.Port)
}

// Type returns backend type ID.
func (e *tcpEndpoint) Type() backend.Type {
	return backend.TCPBackend
}

// Publish returns publish endpoint.
func (e *tcpEndpoint) Publish() (id string) {
	return e.endpoint
}

// Shutdown closes backend.
func (e *tcpEndpoint) Shutdown() {
	e.Arbiter.Shutdown()
	e.Arbiter.Join()
}

func (e *tcpEndpoint) resolve(endpoint string) (addr *net.TCPAddr, err error) {
	v, ok := e.resolveCache.Load(endpoint)
	if !ok || v == nil {
		if addr, err = net.ResolveTCPAddr("tcp", endpoint); err != nil {
			e.log.Errorf("destination \"%v\" not resolved: %v", v, err)
			return nil, err
		}
		e.resolveCache.Store(endpoint, addr)
	} else {
		addr = v.(*net.TCPAddr)
	}
	return
}

// Watch registers callback to receive packet.
func (e *tcpEndpoint) Watch(proc func(backend.Backend, []byte, string)) error {
	if proc != nil {
		e.watch.Store(&proc, proc)
	}
	return nil
}

// IP returns bind IP.
func (e *tcpEndpoint) IP() net.IP {
	return e.bind.IP
}
