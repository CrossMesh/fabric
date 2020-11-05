package tcp

import (
	"encoding/json"
	"errors"

	"github.com/crossmesh/fabric/metanet/backend"
)

var (
	tcpBackendNetworkPath = []string{"network"}

	defaultRawParameterStream = []byte("{}")
)

// BackendManager provides TCP backend.
type BackendManager struct {
	// lock is not required by manager and handled by metanet framework.

	resources backend.ResourceCollection

	endpoints map[string]*tcpEndpoint
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

	m.endpoints = map[string]*tcpEndpoint{}

	if err := m.populateStore(res); err != nil {
		return err
	}

	m.resources = res

	return nil
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

func (m *BackendManager) createEndpoint(ep string) (*tcpEndpoint, error) {
	tx, err := m.resources.StoreTxn(true)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	var basePath []string
	var endpoint *tcpEndpoint
	basePath = append(basePath, tcpBackendNetworkPath...)
	path := append(basePath, ep)
	{
		data, err := tx.Get(path)
		if err != nil {
			return nil, err
		}
		if data != nil { // load.
			endpoint = &tcpEndpoint{}
			if err = endpoint.parameters.Unmarshal(data); err != nil {
				return endpoint, nil
			}
			m.resources.Log().Warnf("%v has corrupted config in store. config will be reset. (err = \"%v\")", err)
		}
	}

	var data []byte

	if err = json.Unmarshal(nil, &endpoint.parameters); err != nil {
		panic(err)
	}
	if data, err = endpoint.parameters.Marshal(); err != nil {
		m.resources.Log().Error("cannot marshal parameters for new endpoint. (err = \"%v\")", err)
		return nil, err
	}
	if err = tx.Set(path, data); err != nil {
		m.resources.Log().Error("fails transaction value write for endpoint creation. (err = \"%v\")", err)
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		m.resources.Log().Error("transaction commit fails for endpoint creation. (err = \"%v\")", err)
		return nil, err
	}

	return endpoint, nil
}

func (m *BackendManager) getEndpoint(ep string, create bool) (endpoint *tcpEndpoint, err error) {
	if endpoint, _ = m.endpoints[ep]; endpoint != nil {
		return endpoint, nil
	}

	if !create {
		return nil, &backend.EndpointNotFoundError{
			Endpoint: backend.Endpoint{
				Endpoint: ep,
				Type:     backend.TCPBackend,
			},
		}
	}

	if endpoint, err = m.createEndpoint(ep); err != nil {
		return nil, err
	}
	m.endpoints[ep] = endpoint

	return endpoint, nil
}

// Activate activates specific endpoint.
// If parameters the endpoint aren't configurated, default parameters will be used.
func (m *BackendManager) Activate(ep string) (err error) {
	endpoint, err := m.getEndpoint(ep, true)
	if err != nil {
		return err
	}
	return endpoint.Activate(
		m.resources.Arbiter(),
		m.resources.Log().WithField("endpoint", ep),
	)
}

// Deactivate deactivates specific endpoint.
func (m *BackendManager) Deactivate(ep string) error {
	endpoint, err := m.getEndpoint(ep, false)
	if err != nil {
		return err
	}
	endpoint.Deactivate()
	return nil
}

// GetBackend returns backend of endpoint.
func (m *BackendManager) GetBackend(ep string) backend.Backend {
	endpoint, _ := m.endpoints[ep]
	if endpoint == nil || !endpoint.Active() {
		return nil
	}
	return nil
}

// ListEndpoints reports all avaliable endpoints.
func (m *BackendManager) ListEndpoints() (endpoints []string) {
	for ep := range m.endpoints {
		endpoints = append(endpoints, ep)
	}
	return
}

// ListActiveEndpoints reports all active endpoints.
func (m *BackendManager) ListActiveEndpoints() (endpoints []string) {
	for ep, instance := range m.endpoints {
		if !instance.Active() {
			continue
		}
		endpoints = append(endpoints, ep)
	}
	return
}
