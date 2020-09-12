package gossip

import (
	"encoding/base64"
	"encoding/json"
	"net"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/sladder"
)

// CrossmeshOverlayParamV1 contains parameters of overlay network driven by general crossmesh encapsulation.
type CrossmeshOverlayParamV1 struct {
	Subnets common.IPNetSet
}

// Clone makes a deep copy.
func (v1 *CrossmeshOverlayParamV1) Clone() (new *CrossmeshOverlayParamV1) {
	return &CrossmeshOverlayParamV1{
		Subnets: v1.Subnets.Clone(),
	}
}

type packCrossmeshOverlayParamV1 struct {
	Subnets json.RawMessage `json:"g,omitempty"`
}

// Encode trys to marshal content to bytes.
func (v1 *CrossmeshOverlayParamV1) Encode() ([]byte, error) {
	bins, err := IPNetSetEncode(v1.Subnets)
	if err != nil {
		return nil, err
	}
	raw := packCrossmeshOverlayParamV1{}
	raw.Subnets = json.RawMessage(base64.RawStdEncoding.EncodeToString(bins))
	return json.Marshal(raw)
}

// Decode trys to unmarshal structure from bytes.
func (v1 *CrossmeshOverlayParamV1) Decode(x []byte) error {
	if x != nil && len(x) < 1 {
		x = []byte("{}")
	}
	raw := packCrossmeshOverlayParamV1{}
	if err := json.Unmarshal(x, &raw); err != nil {
		return err
	}
	bins, err := base64.RawStdEncoding.DecodeString(string(raw.Subnets))
	if err != nil {
		return err
	}
	subnets, err := IPNetSetDecode(bins)
	if err != nil {
		return err
	}
	v1.Subnets = subnets
	return nil
}

// Equal checks whether fields are equal.
func (v1 *CrossmeshOverlayParamV1) Equal(v *CrossmeshOverlayParamV1) bool {
	if v1 == v {
		return true
	}
	if v1 == nil || v == nil {
		return false
	}
	return v1.Subnets.Equal(&v.Subnets)
}

// CrossmeshOverlayParamV1Validator implements CrossmeshOverlayParamV1 param model.
type CrossmeshOverlayParamV1Validator struct{}

// Sync merges states of CrossmeshOverlayParamV1.
func (v1 CrossmeshOverlayParamV1Validator) Sync(local *string, remote string, isConcurrent bool) (bool, error) {
	if local == nil {
		return false, nil
	}
	l, r := CrossmeshOverlayParamV1{}, CrossmeshOverlayParamV1{}
	if err := r.Decode([]byte(remote)); err != nil {
		// reject invalid snapshot.
		return false, nil
	}
	if err := l.Decode([]byte(*local)); err != nil {
		*local = remote
		return true, nil
	}
	if !isConcurrent {
		if r.Equal(&l) {
			return false, nil
		}
		*local = remote
		return true, nil
	}
	// merge.
	if l.Subnets.Merge(&r.Subnets) {
		return false, nil
	}
	bins, err := l.Encode()
	if err != nil {
		return false, err
	}
	*local = string(bins)
	return true, nil
}

// Validate validates CrossmeshOverlayParamV1 structure.
func (v1 CrossmeshOverlayParamV1Validator) Validate(s string) bool {
	r := CrossmeshOverlayParamV1{}
	return r.Decode([]byte(s)) == nil
}

// CrossmeshOverlayParamV1Txn implements KVTransaction for CrossmeshOverlayParamV1
type CrossmeshOverlayParamV1Txn struct {
	oldRaw   string
	old, cur *CrossmeshOverlayParamV1
}

func (t *CrossmeshOverlayParamV1Txn) copyOnWrite() {
	if t.old == t.cur {
		t.cur = t.old.Clone()
	}
}

// Txn starts KVTransaction for CrossmeshOverlayParamV1.
func (v1 CrossmeshOverlayParamV1Validator) Txn(s string) (sladder.KVTransaction, error) {
	txn := CrossmeshOverlayParamV1Txn{}
	txn.oldRaw = s
	txn.cur = &CrossmeshOverlayParamV1{}
	if err := txn.cur.Decode([]byte(s)); err != nil {
		return nil, err
	}
	txn.old = txn.cur
	return &txn, nil
}

// Updated checks whether Txn has updates.
func (t *CrossmeshOverlayParamV1Txn) Updated() bool {
	if t.old == t.cur {
		return false
	}
	return !t.old.Equal(t.cur)
}

// SetRawValue apply new raw value to transaction.
func (t *CrossmeshOverlayParamV1Txn) SetRawValue(x string) error {
	new := CrossmeshOverlayParamV1{}
	if err := new.Decode([]byte(x)); err != nil {
		return err
	}
	t.cur = &new
	return nil
}

// After return current raw value.
func (t *CrossmeshOverlayParamV1Txn) After() string {
	bins, err := t.cur.Encode()
	if err != nil {
		panic(err) // should not happen.
	}
	return string(bins)
}

// Before returns origin raw value.
func (t *CrossmeshOverlayParamV1Txn) Before() string { return t.oldRaw }

// AddSubnet add subnets.
func (t *CrossmeshOverlayParamV1Txn) AddSubnet(subnets ...*net.IPNet) bool {
	t.copyOnWrite()

	newSet := common.IPNetSet(subnets)
	newSet.Build()

	return t.cur.Subnets.Merge(&newSet)
}

// RemoveSubnet remove subnets.
func (t *CrossmeshOverlayParamV1Txn) RemoveSubnet(subnets ...*net.IPNet) bool {
	t.copyOnWrite()
	return t.cur.Subnets.Remove(subnets...)
}
