package gossip

import (
	"bytes"
	"errors"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/sladder"
)

var (
	ErrRevisionTooLong = errors.New("revision is too long")
)

// VersionInfoV1 contains version publication.
type VersionInfoV1 struct {
	MetaVersion uint8

	Major, Minor uint8
	Revision     []byte

	Features version.FeatureSet
}

// Equal reports whether two VersionInfoV1 structure are equal.
func (v1 *VersionInfoV1) Equal(r *VersionInfoV1) bool {
	if v1 == r {
		return true
	}
	if v1 == nil || r == nil {
		return false
	}
	return v1.MetaVersion == r.MetaVersion &&
		v1.Major == r.Major &&
		bytes.Compare(v1.Revision, r.Revision) == 0 &&
		v1.Features.Equal(&r.Features)

}

// Clone makes deep copy.
func (v1 *VersionInfoV1) Clone() *VersionInfoV1 {
	new := &VersionInfoV1{
		MetaVersion: 1,
		Major:       v1.Major,
		Minor:       v1.Minor,
	}
	new.Features = v1.Features.Clone()
	new.Revision = append(new.Revision, v1.Revision...)
	return new
}

// Encode marshals VersionInfoV1.
func (v1 *VersionInfoV1) Encode() (b []byte, err error) {
	revLen := len(v1.Revision)
	if revLen > 0xFF {
		return nil, ErrRevisionTooLong
	}
	b = append(b, 1, v1.Major, v1.Minor)
	b = append(b, uint8(revLen))
	b = append(b, v1.Revision...)
	var featsBin []byte
	if featsBin, err = v1.Features.Encode(); err != nil {
		return nil, err
	}
	b = append(b, featsBin...)
	return
}

// EncodeToString to string.
func (v1 *VersionInfoV1) EncodeToString() (string, error) {
	b, err := v1.Encode()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Validate validates VersionInfoV1.
func (v1 *VersionInfoV1) Validate() error {
	if v1.MetaVersion != 1 {
		return &ModelVersionUnmatchedError{
			Actual:   uint16(v1.MetaVersion),
			Expected: 1,
			Name:     "VersionInfoV1",
		}
	}
	return nil
}

// Decode unmarshals VersionInfoV1.
func (v1 *VersionInfoV1) Decode(b []byte) error {
	if len(b) < 4 {
		return ErrBrokenStream
	}
	metaVersion, major, minor := b[0], b[1], b[2]
	if metaVersion != 1 {
		return &ModelVersionUnmatchedError{
			Actual:   uint16(v1.MetaVersion),
			Expected: 1,
			Name:     "VersionInfoV1",
		}
	}
	revLen := b[3]
	if int(revLen) > len(b)-4 {
		return ErrBrokenStream
	}
	b = b[4:]
	rev := b[:revLen]
	b = b[revLen:]
	features := new(version.FeatureSet)
	if err := features.Decode(b[:]); err != nil {
		return err
	}
	v1.Major, v1.Minor = major, minor
	v1.MetaVersion = metaVersion
	v1.Features = *features
	v1.Revision = rev
	return nil
}

// DecodeString unmarshals VersionInfoV1 from string.
func (v1 *VersionInfoV1) DecodeString(x string) error { return v1.Decode([]byte(x)) }

// VersionInfoValidatorV1 implements operations of VersionInfoV1.
type VersionInfoValidatorV1 struct{}

func (v1 *VersionInfoValidatorV1) sync(local, remote *sladder.KeyValue, isConcurrent bool) (bool, error) {
	if local == nil {
		return false, nil
	}
	if remote == nil {
		return true, nil
	}
	ntx, nerr := createTxn(remote)
	if nerr != nil {
		return false, nerr
	}
	otx, oerr := createTxn(local)
	if oerr != nil {
		local.Value = remote.Value
		return true, nil
	}
	return ntx.Merge(otx, isConcurrent)
}

// Sync merges two VersionInfoV1 structures.
func (v1 *VersionInfoValidatorV1) Sync(local, remote *sladder.KeyValue) (bool, error) {
	return v1.sync(local, remote, false)
}

// SyncEx merges two VersionInfoV1 structures with extra merging properties.
func (v1 *VersionInfoValidatorV1) SyncEx(remote, local *sladder.KeyValue, props sladder.KVMergingProperties) (bool, error) {
	return v1.sync(local, remote, props.Concurrent())
}

// Validate validates VersionInfoV1.
func (v1 *VersionInfoValidatorV1) Validate(kv sladder.KeyValue) bool {
	v1s := VersionInfoV1{}
	return v1s.DecodeString(kv.Value) != nil
}

// VersionInfoV1Txn implements transaction methods of VersionInfoV1.
type VersionInfoV1Txn struct {
	origin   string
	old, new *VersionInfoV1
}

func (t *VersionInfoV1Txn) beforeWrite() {
	if t.old == nil {
		t.new = &VersionInfoV1{}
	}
	if t.old != t.new {
		return
	}
	t.new = t.old.Clone()
}

// Merge merges two transaction.
func (t *VersionInfoV1Txn) Merge(r *VersionInfoV1Txn, isConcurrent bool) (bool, error) {
	t.beforeWrite()
	if !isConcurrent {
		chenged := !r.new.Equal(t.new)
		t.new = r.new.Clone()
		return chenged, nil
	}

	changed := false
	if r.new.MetaVersion != 1 {
		return false, &ModelVersionUnmatchedError{
			Actual:   uint16(r.new.MetaVersion),
			Expected: 1,
			Name:     "VersionInfoV1",
		}
	}
	t.new.MetaVersion = 1
	if v := r.new.Major; v < t.new.Major {
		t.new.Major = v
		changed = true
	}
	if v := r.new.Minor; v < t.new.Minor {
		t.new.Minor = v
		changed = true
	}
	if bytes.Compare(t.new.Revision, r.new.Revision) < 0 {
		if t.new.Revision != nil {
			t.new.Revision = t.new.Revision[:0]
		}
		t.new.Revision = append(t.new.Revision, r.new.Revision...)
		changed = true
	}
	if t.new.Features.Merge(&t.old.Features, false) {
		changed = true
	}

	return changed, nil
}

// Version returns version.
func (t *VersionInfoV1Txn) Version() (uint8, uint8) { return t.new.Major, t.new.Minor }

// Revision returns revision.
func (t *VersionInfoV1Txn) Revision() []byte { return t.new.Revision }

// SetVersion sets major and minor version.
func (t *VersionInfoV1Txn) SetVersion(major, minor uint8) (uint8, uint8) {
	t.beforeWrite()
	oMajor, oMinor := t.new.Major, t.new.Minor
	t.new.Major, t.new.Minor = major, minor
	return oMajor, oMinor
}

// SetRevision sets revision.
func (t *VersionInfoV1Txn) SetRevision(rev []byte) []byte {
	t.beforeWrite()
	old := t.new.Revision
	t.new.Revision = rev
	return old
}

// Updated reports whether transaction has updated.
func (t *VersionInfoV1Txn) Updated() bool {
	return t.new != t.old && !t.new.Equal(t.old)
}

// After returns new value.
func (t *VersionInfoV1Txn) After() string {
	b, err := t.new.Encode()
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Before returns origin raw value.
func (t *VersionInfoV1Txn) Before() string { return t.origin }

// SetRawValue sets new raw value.
func (t *VersionInfoV1Txn) SetRawValue(x string) error {
	t.beforeWrite()
	if err := t.new.DecodeString(x); err != nil {
		return err
	}
	return nil
}

func createTxn(kv *sladder.KeyValue) (*VersionInfoV1Txn, error) {
	txn := VersionInfoV1Txn{origin: kv.Value}
	if err := txn.SetRawValue(kv.Value); err != nil {
		return nil, err
	}
	return &txn, nil
}

// Txn starts VersionInfoV1 transaction.
func (v1 *VersionInfoValidatorV1) Txn(kv sladder.KeyValue) (sladder.KVTransaction, error) {
	return createTxn(&kv)
}
