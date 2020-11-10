package tcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/metanet/backend"
)

var (
	tcpBackendNetworkPath = []string{"network"}

	defaultRawParameterStream = []byte("{}")
)

// BackendManager provides TCP backend.
type BackendManager struct {
	lock sync.RWMutex

	// lock is not required by manager and handled by metanet framework.
	watchers sync.Map

	resources backend.ResourceCollection

	endpoints map[string]*tcpEndpoint

	publishEndpoints map[string]struct{}
}

// Watch registers callback to receive packet.
func (m *BackendManager) Watch(proc func(backend.Backend, []byte, string)) error {
	if proc != nil {
		m.watchers.Store(&proc, proc)
	}
	return nil
}

// Type returns TCPBackend type.
func (m *BackendManager) Type() backend.Type {
	return backend.TCPBackend
}

// Init initializes TCP backend.
func (m *BackendManager) Init(res backend.ResourceCollection) error {
	if res == nil {
		return errors.New("got nil resource collection")
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if m.resources != nil {
		panic("init twice.")
	}

	m.endpoints = map[string]*tcpEndpoint{}

	if err := m.populateStore(res); err != nil {
		return err
	}

	m.activate()

	m.resources = res

	return nil
}

func (m *BackendManager) activate() {
	for bind, endpoint := range m.endpoints {
		if endpoint == nil {
			continue
		}
		if endpoint.parameters.autoBind {
			endpoint.Activate(
				m.resources.Arbiter(),
				m.resources.Log().WithField("bind", bind),
			)
		}
	}
}

func (m *BackendManager) populateStore(res backend.ResourceCollection) error {
	tx, err := res.StoreTxn(true)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	var basePath []string
	var needReset []string

	needRewrite := false

	basePath = append(basePath, tcpBackendNetworkPath...)
	// load config
	if err = tx.Range(basePath, func(path []string, data []byte) bool {
		publish := path[len(path)-1]
		endpoint := &tcpEndpoint{}
		if len(data) == 0 {
			data = defaultRawParameterStream
		}
		if err = endpoint.parameters.Unmarshal(data); err != nil {
			res.Log().Warnf("%v has corrupted config in store. config will be reset. (err = \"%v\")", err)
			needReset = append(needReset, publish)
			if err = endpoint.parameters.Unmarshal(nil); err != nil {
				panic(err)
			}
		}
		m.endpoints[publish] = endpoint
		return true
	}); err != nil {
		res.Log().Error("cannot range over stored networks. (err = \"%v\")", err)
		return err
	}

	if len(needReset) > 0 {
		needRewrite = true
	}
	for _, name := range needReset {
		ep := m.endpoints[name]
		data, err := json.Marshal(&ep.parameters)
		if err != nil {
			res.Log().Error("cannot encode network parameters when trying to reset config. (err = \"%v\")", err)
			return err
		}

		path := append(basePath, name)
		if err = tx.Set(path, data); err != nil {
			res.Log().Error("failed to write the store to reset config. (err = \"%v\")", err)
			return err
		}
	}

	if needRewrite {
		if err = tx.Commit(); err != nil {
			res.Log().Error("failed to commit transaction to reset config. (err = \"%v\")", err)
			return err
		}
	} else if err = tx.Rollback(); err != nil {
		panic(err)
	}

	return nil
}

func (m *BackendManager) unpublishAddr(addr *net.TCPAddr) {
	if addr == nil {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	ep := addr.String()

	_, published := m.publishEndpoints[ep]
	if !published {
		return
	}
	delete(m.publishEndpoints, ep)

	m.resubmitEndpoint()
}

func (m *BackendManager) resubmitEndpoint() {
	var endpoints []string

	for ep := range m.publishEndpoints {
		endpoints = append(endpoints, ep)
	}
	m.resources.EndpointPublisher().UpdatePublish(endpoints...)
}

func (m *BackendManager) publishAddr(addr *net.TCPAddr) {
	if addr == nil {
		return
	}
	m.lock.Lock()
	defer m.lock.Unlock()

	if addr.IP.IsLoopback() || // ignore loopback.
		addr.IP.IsMulticast() || // ignore multicast.
		addr.IP.IsLinkLocalMulticast() ||
		addr.IP.IsUnspecified() { // ignore unspecified addr.
		m.resources.Log().Warn("ignore invalid TCP publish endpoint address \"%v\".", addr.IP)
		return
	}

	ep := addr.String()

	_, published := m.publishEndpoints[ep]
	if published {
		return
	}

	m.publishEndpoints[ep] = struct{}{}
	m.resubmitEndpoint()
}

func (m *BackendManager) createEndpoint(tx common.StoreTxn, ep string) (endpoint *tcpEndpoint, err error) {
	var basePath []string
	basePath = append(basePath, tcpBackendNetworkPath...)
	path := append(basePath, ep)
	{
		data, err := tx.Get(path)
		if err != nil {
			return nil, err
		}
		if data != nil { // load.
			endpoint = newTCPEndpoint(m)
			if err = endpoint.parameters.Unmarshal(data); err == nil {
				return endpoint, nil
			}
			m.resources.Log().Warnf("%v has corrupted config in store. config will be reset. (err = \"%v\")", err)
		}
	}

	var data []byte

	endpoint.parameters.ResetDefault()
	if data, err = endpoint.parameters.Marshal(); err != nil {
		m.resources.Log().Error("cannot marshal parameters for new endpoint. (err = \"%v\")", err)
		return nil, err
	}
	if err = tx.Set(path, data); err != nil {
		m.resources.Log().Error("fails transaction value write for endpoint creation. (err = \"%v\")", err)
		return nil, err
	}

	return endpoint, nil
}

func (m *BackendManager) getEndpointOrCreate(tx common.StoreTxn, ep string, create bool) (endpoint *tcpEndpoint, isNew bool, err error) {
	if _, err = net.ResolveTCPAddr("tcp", ep); err != nil {
		return nil, false, &common.ParamError{
			Msg: fmt.Sprintf("cannot resolve bind address \"%v\". the endpoint may be invalid. (resolve err = \"%v\")", ep, err),
		}
	}

	if endpoint, _ = m.endpoints[ep]; endpoint != nil {
		return endpoint, false, nil
	}

	if !create || tx == nil {
		return nil, false, &backend.EndpointNotFoundError{
			Endpoint: backend.Endpoint{
				Endpoint: ep,
				Type:     backend.TCPBackend,
			},
		}
	}

	if endpoint, err = m.createEndpoint(tx, ep); err != nil {
		return nil, false, err
	}
	tx.OnCommit(func() {
		m.endpoints[ep] = endpoint
	})

	return endpoint, true, nil
}

func (m *BackendManager) getEndpoint(ep string) (endpoint *tcpEndpoint, err error) {
	endpoint, _, err = m.getEndpointOrCreate(nil, ep, false)
	return endpoint, err
}

func (m *BackendManager) setAutoBindEndpoint(tx common.StoreTxn, endpoint *tcpEndpoint, auto bool) error {
	if endpoint.parameters.autoBind == auto {
		return nil
	}
	endpoint.parameters.autoBind = auto
	data, err := endpoint.parameters.Marshal()
	if err != nil {
		m.resources.Log().Error("cannot marshal endpoint parameters. (err = \"%v\")", err)
		return err
	}

	var basePath []string
	basePath = append(basePath, tcpBackendNetworkPath...)
	path := append(basePath, endpoint.parameters.Bind)
	if err = tx.Set(path, data); err != nil {
		m.resources.Log().Error("failed to write parameters to store. (err = \"%v\")", err)
		return err
	}

	return nil
}

// Bind binds specific endpoint.
func (m *BackendManager) Bind(ep string, permanent bool) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	endpoint, err := m.getEndpoint(ep)
	if err != nil {
		return err
	}
	var tx common.StoreTxn
	if endpoint == nil || (!endpoint.parameters.autoBind && permanent) {
		tx, err = m.resources.StoreTxn(true)
		if err != nil {
			return err
		}
		defer func() {
			if tx != nil && err != nil {
				if rerr := tx.Rollback(); rerr != nil {
					panic(rerr)
				}
			}
		}()
	}

	if endpoint == nil {
		endpoint, _, err = m.getEndpointOrCreate(tx, ep, true)
		if err != nil {
			return err
		}
	}

	if permanent && !endpoint.parameters.autoBind {
		if err = m.setAutoBindEndpoint(tx, endpoint, true); err != nil {
			return err
		}
	}

	if tx != nil {
		if err = tx.Commit(); err != nil {
			m.resources.Log().Error("transaction commit fails for endpoint creation. (err = \"%v\")", err)
			return err
		}
	}

	if endpoint.Active() {
		return nil
	}

	endpoint.Activate(
		m.resources.Arbiter(),
		m.resources.Log().WithField("bind", ep),
	)

	return nil
}

// Unbind unbinds specific endpoint.
func (m *BackendManager) Unbind(ep string, permanent bool) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	endpoint, err := m.getEndpoint(ep)
	if err != nil {
		return err
	}
	if endpoint == nil {
		return nil
	}

	if permanent && endpoint.parameters.autoBind {
		tx, err := m.resources.StoreTxn(true)
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				if rerr := tx.Rollback(); rerr != nil {
					panic(rerr)
				}
			}
		}()
		if err = m.setAutoBindEndpoint(tx, endpoint, false); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			m.resources.Log().Error("transaction commit fails for saving endpoint bind states. (err = \"%v\")", err)
			return err
		}
	}

	endpoint.Deactivate()
	return nil
}

// GetBackend returns backend of endpoint.
func (m *BackendManager) GetBackend(ep string) backend.Backend {
	m.lock.RLock()
	// TODO(xutao): sync.Mutex
	endpoint, _ := m.endpoints[ep]
	m.lock.RUnlock()

	if endpoint == nil || !endpoint.Active() {
		return nil
	}
	return nil
}
