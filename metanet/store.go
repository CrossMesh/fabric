package metanet

import (
	"github.com/crossmesh/fabric/common"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

var (
	backendStorePath = []string{"backend"}
)

type storedActiveEndpoints struct {
	Endpoints []string `json:"eps"`
}

type managerResourceCollection struct {
	log     *logging.Entry
	arbiter *arbit.Arbiter
	store   common.Store
}

func (c *managerResourceCollection) StoreTxn(writable bool) (common.StoreTxn, error) {
	return c.store.Txn(writable)
}

func (c *managerResourceCollection) Log() *logging.Entry { return c.log }

func (c *managerResourceCollection) Arbiter() *arbit.Arbiter { return c.arbiter }
