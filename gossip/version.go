package gossip

import (
	"errors"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/sladder"
)

var (
	ErrVersionTooLong = errors.New("version is too long")
)

// VersionInfoV1 contains version publication.
type VersionInfoV1 struct {
	MetaVersion uint8

	Version version.SemVer

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
		v1.Version.Equal(&r.Version) &&
		v1.Features.Equal(&r.Features)
}

// Clone makes deep copy.
func (v1 *VersionInfoV1) Clone() *VersionInfoV1 {
	new := &VersionInfoV1{
		MetaVersion: 1,
		Version:     v1.Version,
	}
	new.Features = v1.Features.Clone()
	new.Version = v1.Version
	return new
}

// Encode marshals VersionInfoV1.
func (v1 *VersionInfoV1) Encode() (b []byte, err error) {
	semver := v1.Version.String()
	verLen := len(semver)
	if verLen > 0xFF {
		return nil, ErrVersionTooLong
	}
	b = append(b, 1, uint8(verLen))
	b = append(b, []byte(semver)...)
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
	if len(b) == 0 {
		v1.MetaVersion = 1
		if _, err := v1.Features.Decode(b); err != nil {
			return err
		}
		return nil
	}
	if len(b) < 2 {
		return ErrBrokenStream
	}
	metaVersion := b[0]
	if metaVersion != 1 {
		return &ModelVersionUnmatchedError{
			Actual:   uint16(v1.MetaVersion),
			Expected: 1,
			Name:     "VersionInfoV1",
		}
	}

	verLen := b[1]
	if int(verLen) > len(b)-2 {
		return ErrBrokenStream
	}
	b = b[2:]
	ver := &version.SemVer{}
	if !ver.Parse(string(b[:verLen])) {
		return ErrBrokenStream
	}
	b = b[verLen:]

	features := new(version.FeatureSet)
	if _, err := features.Decode(b[:]); err != nil {
		return err
	}

	v1.MetaVersion = metaVersion
	v1.Features = *features
	v1.Version = *ver
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
	changed, merr := otx.Merge(ntx, isConcurrent)
	if merr != nil {
		return false, merr
	}
	if changed {
		local.Value = otx.After()
	}
	return changed, nil
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
	return v1s.DecodeString(kv.Value) == nil
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
		changed := !r.new.Equal(t.new)
		t.new = r.new.Clone()
		return changed, nil
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

	// merge semver.
	remoteAccepted := false
	if r.new.Version.Equal(&t.new.Version) {
		l, r := t.new.Version.Matadata, r.new.Version.Matadata
		if len(l) < len(r) {
			remoteAccepted = true

		} else if len(l) == len(r) {
			for i := 0; i < len(l); i++ {
				if l[i] < r[i] {
					remoteAccepted = true
					break
				}
			}
		}
	} else if t.new.Version.Less(&r.new.Version) {
		remoteAccepted = true
	}
	if remoteAccepted {
		t.new.Version = *r.new.Version.Clone()
		changed = true
	}

	// merge feature set.
	if t.new.Features.Merge(&r.new.Features, false) {
		changed = true
	}

	return changed, nil
}

// Version returns version.
func (t *VersionInfoV1Txn) Version() *version.SemVer { return t.new.Version.Clone() }

// SetVersion sets major and minor version.
func (t *VersionInfoV1Txn) SetVersion(v *version.SemVer) {
	if v == nil {
		return
	}
	t.beforeWrite()
	t.new.Version = *v.Clone()
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

// Enable enables feature.
func (t *VersionInfoV1Txn) Enable(feature int) bool {
	t.beforeWrite()
	return t.new.Features.Enable(feature)
}

// Disable disables feature.
func (t *VersionInfoV1Txn) Disable(feature int) bool {
	t.beforeWrite()
	return t.new.Features.Disable(feature)
}

// Enabled reports whether feature with specific ID is enabled.
func (t *VersionInfoV1Txn) Enabled(feature int) bool {
	return t.new.Features.Enabled(feature)
}

// AssignFeatures enables features by update entire FeatureSet.
func (t *VersionInfoV1Txn) AssignFeatures(features ...int) {
	t.beforeWrite()
	t.new.Features = version.FeatureSet{}
	for _, feat := range features {
		t.new.Features.Enable(feat)
	}
}

// VersionInfoV1 returns clone of current VersionInfoV1 structure.
func (t *VersionInfoV1Txn) VersionInfoV1() *VersionInfoV1 { return t.new.Clone() }

func createTxn(kv *sladder.KeyValue) (*VersionInfoV1Txn, error) {
	txn := VersionInfoV1Txn{origin: kv.Value}
	if err := txn.SetRawValue(kv.Value); err != nil {
		return nil, err
	}
	txn.old = txn.new
	return &txn, nil
}

// Txn starts VersionInfoV1 transaction.
func (v1 *VersionInfoValidatorV1) Txn(kv sladder.KeyValue) (sladder.KVTransaction, error) {
	return createTxn(&kv)
}
