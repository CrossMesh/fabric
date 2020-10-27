package metanet

// StoreTxn implements transaction.
type StoreTxn interface {
	Writable() bool

	Set(path []string, data []byte) error
	Get(path []string) ([]byte, error)
	Delete(path []string) error
	Range(path []string, visit func(path []string, data []byte) bool) error // not recursive

	OnCommit(func())

	Commit() error
	Rollback() error
}

// Store stores metadata.
type Store interface {
	Txn(writable bool) (StoreTxn, error)
}

var (
	backendStorePath = []string{"backend"}
)

type storedActiveEndpoints struct {
	endpoints []string `json:"eps"`
}
