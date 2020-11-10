package backend

import (
	"github.com/crossmesh/fabric/common"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

// EndpointPublisher contains endpoint publish interface for backend manager.
type EndpointPublisher interface {
	UpdatePublish(...string)
}

// ResourceCollection defines resources required by manager.
type ResourceCollection interface {
	StoreTxn(writable bool) (common.StoreTxn, error)
	Log() *logging.Entry
	Arbiter() *arbit.Arbiter

	EndpointPublisher() EndpointPublisher
}

// Manager manages a set of backends of type.
type Manager interface {
	Type() Type

	Init(ResourceCollection) error
	Watch(func(Backend, []byte, string)) error

	GetBackend(string) Backend
}

// ParameterizedManager accepts parameter configuration.
//type ParameterizedManager interface {
//	SetParams(endpoint string, args []string) error
//	ShowParams(...string) ([]map[string]string, error)
//}
