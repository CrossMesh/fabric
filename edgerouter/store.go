package edgerouter

// OptionStoreTxn implements transaction for modifing overlay network options.
type OptionStoreTxn interface {
	Writable() bool

	Set(networkID uint32, name string, data []byte) error
	Get(networkID uint32, name string) ([]byte, error)
	Delete(networkID uint32, name string) error

	Commit() error
	Rollback() error
}

// OptionStore persists options of network.
type OptionStore interface {
	Txn(writable bool) (*OptionStoreTxn, error)
}

// UserOptionTxn provides methods for human to modify network options.
type UserOptionTxn interface {
	Writable() bool

	Add(networkID uint32, name string, values ...string) error
	Remove(networkID uint32, name string, values ...string) error
	Get(networkID uint32, name string) (string, error)

	Commit() error
	Rollback() error
}
