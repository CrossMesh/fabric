package common

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

// Store defines simple KV store interface.
type Store interface {
	Txn(writable bool) (StoreTxn, error)
}

// SubpathStore wraps Store to enforce storing to configurated subpath.
type SubpathStore struct {
	Store
	Prefix []string
}

// Txn starts new StoreTxn.
func (s *SubpathStore) Txn(writable bool) (txn StoreTxn, err error) {
	if txn, err = s.Store.Txn(writable); err != nil {
		return nil, err
	}
	txn = &SubpathStoreTxn{
		StoreTxn: txn,
		store:    s,
	}
	return txn, nil
}

// SubpathStoreTxn implements transaction wrapper for SubpathStore.
type SubpathStoreTxn struct {
	StoreTxn

	store *SubpathStore
}

func (t *SubpathStoreTxn) buildActualPath(path []string) (actual []string) {
	if len(t.store.Prefix) < 1 {
		return path
	}
	actual = append(actual, t.store.Prefix...)
	actual = append(actual, path...)
	return
}

// Set sets value for path of key.
func (t *SubpathStoreTxn) Set(path []string, data []byte) error {
	path = t.buildActualPath(path)
	return t.Set(path, data)
}

// Get gets value for a path of key.
func (t *SubpathStoreTxn) Get(path []string) ([]byte, error) {
	path = t.buildActualPath(path)
	return t.Get(path)
}

// Delete removes key for path of key.
func (t *SubpathStoreTxn) Delete(path []string) error {
	path = t.buildActualPath(path)
	return t.Delete(path)
}

// Range iterates over key values with a path.
func (t *SubpathStoreTxn) Range(path []string, visit func([]string, []byte) bool) error {
	actualPath := t.buildActualPath(path)
	prefixLen := len(actualPath) - len(path)
	return t.Range(actualPath, func(keyPath []string, data []byte) bool {
		keyPath = keyPath[prefixLen:]
		return visit(keyPath, data)
	})
}
