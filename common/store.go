package common

import (
	"errors"

	"go.etcd.io/bbolt"
)

var (
	ErrInvalidStorePath  = errors.New("invalid store path")
	ErrForEachTerminated = errors.New("terminated for-each loop")
)

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

type boltDBStore struct {
	db *bbolt.DB
}

// BoltDBStore wraps boltdb as store.
func BoltDBStore(db *bbolt.DB) Store {
	return &boltDBStore{db: db}
}

type boltStoreTxn struct {
	tx *bbolt.Tx
}

func (t *boltDBStore) Txn(writable bool) (StoreTxn, error) {
	tx, err := t.db.Begin(writable)
	if err != nil {
		return nil, err
	}
	return &boltStoreTxn{tx}, nil
}

func (t *boltStoreTxn) OnCommit(fn func()) { t.OnCommit(fn) }
func (t *boltStoreTxn) Commit() error      { return t.Commit() }
func (t *boltStoreTxn) Rollback() error    { return t.Rollback() }
func (t *boltStoreTxn) Writable() bool     { return t.tx.Writable() }

func (t *boltStoreTxn) resolveBucketPath(path []string, createBucket bool) (bucket *bbolt.Bucket, err error) {
	if len(path) < 1 {
		return nil, ErrInvalidStorePath
	}

	key := []byte(path[0])
	if bucket = t.tx.Bucket(key); bucket == nil {
		if !createBucket {
			return nil, nil
		}
		if bucket, err = t.tx.CreateBucket(key); err != nil {
			return nil, err
		}
	}

	// down
	for i := 1; i < len(path); i++ {
		key = []byte(path[i])
		nextBucket := bucket.Bucket(key)
		if nextBucket == nil {
			if !createBucket {
				return nil, nil
			}

			if nextBucket, err = bucket.CreateBucket(key); err != nil {
				return nil, err
			}
		}
		bucket = nextBucket
	}

	return bucket, nil
}

func (t *boltStoreTxn) resolvePath(path []string, createBucket bool) (bucket *bbolt.Bucket, valueKey string, err error) {
	if len(path) < 2 {
		return nil, "", ErrInvalidStorePath
	}

	lengthPath := len(path)
	if bucket, err = t.resolveBucketPath(path[:lengthPath-1], createBucket); err != nil {
		return nil, "", err
	}

	return bucket, path[len(path)-1], nil
}

func (t *boltStoreTxn) Set(path []string, data []byte) error {
	bucket, key, err := t.resolvePath(path, true)
	if err != nil {
		return err
	}
	return bucket.Put([]byte(key), data)
}

func (t *boltStoreTxn) Get(path []string) ([]byte, error) {
	bucket, key, err := t.resolvePath(path, false)
	if err != nil {
		return nil, err
	}
	if bucket == nil {
		return nil, nil
	}
	return bucket.Get([]byte(key)), nil
}

func (t *boltStoreTxn) Delete(path []string) error {
	bucket, key, err := t.resolvePath(path, false)
	if err != nil {
		return err
	}
	if bucket == nil {
		return nil
	}
	return bucket.Delete([]byte(key))
}

func (t *boltStoreTxn) Range(path []string, visit func([]string, []byte) bool) error {
	if visit == nil {
		return nil
	}
	bucket, err := t.resolveBucketPath(path, false)
	if err != nil {
		return err
	}
	if bucket == nil {
		return nil
	}

	var basePath []string
	basePath = append(basePath, path...)
	if err = bucket.ForEach(func(k, v []byte) error {
		if !visit(append(basePath, string(k)), v) {
			return ErrForEachTerminated
		}
		return nil
	}); err != nil && err != ErrForEachTerminated {
		return err
	}
	return nil
}
