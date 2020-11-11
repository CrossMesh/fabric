package gossip

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/sladder"
)

type NetworkNotFoundError struct {
	want driver.NetworkID
}

func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("network %v not found", e.want.String())
}

const (
	// VersionOverlayNetworksV1 is version value of VersionOverlayNetworksV1 data model.
	VersionOverlayNetworksV1 = uint16(1)
)

// OverlayNetworkV1 contains joined overlay networks of peer.
type OverlayNetworkV1 struct {
	Params map[string][]byte
}

// Clone makes deep copy.
func (v1 *OverlayNetworkV1) Clone() *OverlayNetworkV1 {
	new := &OverlayNetworkV1{}
	for k, d := range v1.Params {
		new.Params[k] = d
	}
	return new
}

// Equal checks whether fields in two OverlayNetworkV1 are equal.
func (v1 *OverlayNetworkV1) Equal(x *OverlayNetworkV1) bool {
	if v1 == x {
		return true
	}
	if v1 == nil || x == nil {
		return false
	}
	if len(v1.Params) != len(x.Params) {
		return false
	}
	for k, d := range v1.Params {
		rd, has := x.Params[k]
		if !has {
			return false
		}
		if bytes.Compare(rd, d) != 0 {
			return false
		}
	}
	return true
}

// Encode marshals OverlayNetworkV1.
func (v1 *OverlayNetworkV1) Encode() (bin []byte, err error) {
	numOfOptions := len(v1.Params)
	if numOfOptions > 0xFF {
		return nil, errors.New("too many options")
	}
	bin = append(bin, uint8(numOfOptions))
	numOfEncoded := 0
	for k, d := range v1.Params {
		numOfEncoded++
		length := len(k)
		if length > 0xFF {
			return nil, fmt.Errorf("key \"%v\" is too long", k)
		}
		bin = append(bin, uint8(length))
		if length = len(d); length > 0x7FFFFFFF {
			return nil, fmt.Errorf("value with key \"%v\" is of length %v and cannot be encoded", k, length)
		}
		bin = append(bin, []byte(k)...)
		if length < 0x7F {
			bin = append(bin, uint8(length))
		} else { // extended
			encoded := (uint32(length) & 0xFFFFFF80) << 1
			encoded |= (uint32(length) & 0x7F) | 0x80
			bin = append(bin, 0x00, 0x00, 0x00, 0x00)
			binary.LittleEndian.PutUint32(bin[len(bin)-4:], encoded)
		}
		bin = append(bin, d...)
	}
	if numOfEncoded != numOfOptions {
		return nil, errors.New("data race in OverlayNetworkV1 Encode()")
	}
	return bin, nil
}

// Decode unmarshals OverlayNetworkV1.
func (v1 *OverlayNetworkV1) Decode(bin []byte) error {
	new := make(map[string][]byte)
	if len(bin) < 1 {
		v1.Params = new
		return nil
	}
	numOfOptions := bin[0]
	bin = bin[1:]
	for ; numOfOptions > 0; numOfOptions-- {
		if len(bin) < 1 {
			return common.ErrBrokenStream
		}
		// key
		length := uint32(bin[0])
		bin = bin[1:]
		if len(bin) < int(length) {
			return common.ErrBrokenStream
		}
		key := string(bin[:length])
		bin = bin[length:]
		// data
		length = uint32(bin[0])
		if length&0x80 != 0 { // extended length.
			raw := binary.LittleEndian.Uint32(bin[0:4])
			length = 0
			length |= raw & 0x7F
			length |= (raw & 0xFFFFFF) >> 1
			bin = bin[4:]
		} else {
			bin = bin[1:]
		}
		if len(bin) < int(length) {
			return common.ErrBrokenStream
		}
		d := bin[:length]
		bin = bin[length:]
		new[key] = d
	}

	v1.Params = new

	// consumed length is not exported. export it later in need.
	return nil
}

type packOverlayNetworkV1 struct {
	driver.NetworkID
	Params string `json:"p"`
}

// OverlayNetworksV1 contains joined overlay networks of peer.
type OverlayNetworksV1 struct {
	Version     uint16 `json:"v,omitempty"`
	UnderlayID  int32  `json:"u,omitempty"`
	UnderlayIPs common.IPNetSet
	Networks    map[driver.NetworkID]*OverlayNetworkV1 `json:"nets,omitempty"`
}

const (
	// DefaultOverlayNetworkKey is default key name for OverlayNetworks model on gossip framework.
	DefaultOverlayNetworkKey = "overlay_network"
)

// Clone makes a new deepcopy.
func (v1 *OverlayNetworksV1) Clone() (new *OverlayNetworksV1) {
	new = &OverlayNetworksV1{}
	new.Version = v1.Version
	if v1.Networks != nil {
		new.Networks = map[driver.NetworkID]*OverlayNetworkV1{}
		for netID, cfg := range v1.Networks {
			new.Networks[netID] = cfg.Clone()
		}
	}
	return
}

// Equal checks whether contents of two OverlayNetworksV1 are equal.
func (v1 *OverlayNetworksV1) Equal(x *OverlayNetworksV1) bool {
	if v1 == x {
		return true
	}
	if v1 == nil || x == nil {
		return false
	}
	if v1.Version != x.Version ||
		len(v1.Networks) != len(x.Networks) {
		return false
	}
	for netID, cfg := range v1.Networks {
		rcfg, exists := x.Networks[netID]
		if !exists {
			return false
		}
		if !cfg.Equal(rcfg) {
			return false
		}
	}
	return true
}

type packOverlayNetworksV1 struct {
	Version    uint16                 `json:"v,omitempty"`
	Networks   []packOverlayNetworkV1 `json:"nets,omitempty"`
	UnderlayID int32                  `json:"uid,omitempty"`
}

// EncodeToString trys to marshal structure to string.
func (v1 *OverlayNetworksV1) EncodeToString() (string, error) {
	pack := packOverlayNetworksV1{Version: VersionOverlayNetworksV1}
	pack.Networks = make([]packOverlayNetworkV1, 0, len(v1.Networks))
	for netID, cfg := range v1.Networks {
		raw, err := cfg.Encode()
		if err != nil {
			return "", err
		}
		params := base64.StdEncoding.EncodeToString(raw)
		pack.Networks = append(pack.Networks, packOverlayNetworkV1{
			Params:    params,
			NetworkID: netID,
		})
	}
	b, err := json.Marshal(&pack)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeString try to unmarshal contents from string
func (v1 *OverlayNetworksV1) DecodeString(s string) (err error) {
	if s == "" {
		s = "{\"v\": 1}"
	}
	pack := packOverlayNetworksV1{}
	if err = json.Unmarshal([]byte(s), &pack); err != nil {
		return err
	}
	netMap := make(map[driver.NetworkID]*OverlayNetworkV1, len(pack.Networks))
	for _, net := range pack.Networks {
		if _, dup := netMap[net.NetworkID]; dup {
			return fmt.Errorf("OverlayNetworksV1 has broken integrity. Duplicated network ID %v found", net.NetworkID)
		}
		raw, err := base64.StdEncoding.DecodeString(net.Params)
		if err != nil {
			return err
		}
		o := &OverlayNetworkV1{}
		netMap[net.NetworkID] = o
		if err = o.Decode(raw); err != nil {
			return err
		}
	}
	v1.Version = pack.Version
	v1.Networks = netMap
	return nil
}

// Validate validates fields.
func (v1 *OverlayNetworksV1) Validate() error {
	if actual := v1.Version; actual != VersionOverlayNetworksV1 {
		return &common.ModelVersionUnmatchedError{Name: "OverlayNetworksV1", Actual: actual, Expected: VersionOverlayNetworksV1}
	}
	return nil
}

// DecodeStringAndValidate trys to unmarshal structure from string and do validation.
func (v1 *OverlayNetworksV1) DecodeStringAndValidate(s string) error {
	if err := v1.DecodeString(s); err != nil {
		return err
	}
	return v1.Validate()
}

// OverlayNetworksValidatorV1 implements OverlayNetworksV1 model.
type OverlayNetworksValidatorV1 struct {
	lock sync.RWMutex
}

// Sync merges state of OverlayNetworksV1 to local.
func (v1 *OverlayNetworksValidatorV1) Sync(local, remote *sladder.KeyValue) (changed bool, err error) {
	return v1.sync(local, remote, false)
}

func (v1 *OverlayNetworksValidatorV1) sync(local, remote *sladder.KeyValue, isConcurrent bool) (changed bool, err error) {
	if remote == nil { // Deletion.
		return true, nil
	}
	if !isConcurrent {
		// just copy.
		local.Value = remote.Value
		return true, nil
	}

	lv, rv := OverlayNetworksV1{}, OverlayNetworksV1{}
	if changed, err = v1.presync(local, remote, &lv, &rv); changed || err != nil {
		return
	}

	// Our network model assumes peer only modifies params of itself.
	// This ensures that no concurrent case will appear, but we deal with concurrent cases for safety.
	// Concurrent merging is implemented below.

	changed = false

	for netID, net := range rv.Networks { // update.
		origin, exists := lv.Networks[netID]
		if !exists {
			lv.Networks[netID] = net
			changed = true
			continue
		}

		if len(origin.Params) < 1 {
			origin.Params = net.Params
			if len(origin.Params) > 0 {
				changed = true
			}
			continue
		}

		for k, d := range net.Params {
			od, exists := origin.Params[k]
			if !exists || bytes.Compare(d, od) > 0 {
				// only for achieving totally ordering.
				origin.Params[k] = d
				changed = true
			}
		}
	}
	{
		new, err := lv.EncodeToString()
		if err != nil {
			return false, nil
		}
		local.Value = new
	}

	return changed, nil
}

func (v1 *OverlayNetworksValidatorV1) presync(
	local, remote *sladder.KeyValue,
	lv, rv *OverlayNetworksV1) (changed bool, err error) {
	if err = lv.DecodeStringAndValidate(local.Value); err != nil {
		// Since a invalid remote snapshot won't be accapted, this condition hardly can be reached.
		// In this case, however, synchronization will make no progress forever if we returns error simply.
		// Reseting raw value to the initial will prevent this happen..
		local.Value = "{\"v\": 1}"
		changed = true
	}
	if err = rv.DecodeStringAndValidate(remote.Value); err != nil {
		if changed {
			err = nil
		}
		return
	}
	return false, nil
}

// Validate validates fields.
func (v1 *OverlayNetworksValidatorV1) Validate(kv sladder.KeyValue) bool {
	on1 := OverlayNetworksV1{}
	if on1.DecodeString(kv.Value) != nil {
		return false
	}
	return true
}

// SyncEx merges state of OverlayNetworksV1 to local by respecting extended properties.
func (v1 *OverlayNetworksValidatorV1) SyncEx(local, remote *sladder.KeyValue,
	props sladder.KVMergingProperties) (changed bool, err error) {
	if props.Concurrent() && remote == nil {
		// existance wins.
		return false, nil
	}
	return v1.sync(local, remote, props.Concurrent())
}

// OverlayNetworksV1Txn implements KVTransaction of OverlayNetworksV1.
type OverlayNetworksV1Txn struct {
	validator *OverlayNetworksValidatorV1

	oldRaw   string
	old, cur *OverlayNetworksV1
}

// CloneCurrent makes deepcopy of current OverlayNetworksV1 structure.
func (t *OverlayNetworksV1Txn) CloneCurrent() *OverlayNetworksV1 { return t.cur.Clone() }

// Txn starts KVTransaction of OverlayNetworksV1.
func (v1 *OverlayNetworksValidatorV1) Txn(kv sladder.KeyValue) (sladder.KVTransaction, error) {
	txn := &OverlayNetworksV1Txn{
		oldRaw:    kv.Value,
		validator: v1,
	}
	if err := txn.SetRawValue(kv.Value); err != nil {
		return nil, err
	}
	txn.old = txn.cur
	return txn, nil
}

func (t *OverlayNetworksV1Txn) copyOnWrite() {
	if t.old != nil && t.old == t.cur {
		t.cur = t.old.Clone()
	}
}

// SetRawValue set new raw value.
func (t *OverlayNetworksV1Txn) SetRawValue(x string) error {
	new := &OverlayNetworksV1{}
	if err := new.DecodeStringAndValidate(x); err != nil {
		return err
	}
	t.cur = new
	return nil
}

// Before returns origin raw value.
func (t *OverlayNetworksV1Txn) Before() string { return t.oldRaw }

// After return current raw value.
func (t *OverlayNetworksV1Txn) After() string {
	cur := t.cur.Clone()
	s, err := cur.EncodeToString()
	if err != nil {
		panic(err) // should not happen.
	}
	return s
}

// Updated checks whether value is updated.
func (t *OverlayNetworksV1Txn) Updated() bool {
	if t.old == t.cur {
		return false
	}
	return !t.old.Equal(t.cur)
}

// Version returns current value of version field in OverlayNetworksV1.
func (t *OverlayNetworksV1Txn) Version() uint16 { return t.cur.Version }

// RemoveNetwork removes networks according to a set of NetworkID.
func (t *OverlayNetworksV1Txn) RemoveNetwork(ids ...driver.NetworkID) {
	if len(ids) < 1 {
		return
	}
	t.copyOnWrite()
	for _, netID := range ids {
		delete(t.cur.Networks, netID)
	}
}

// AddNetwork adds networks according to a set of NetworkID.
func (t *OverlayNetworksV1Txn) AddNetwork(ids ...driver.NetworkID) (err error) {
	if len(ids) < 1 {
		return nil
	}
	t.copyOnWrite()
	for _, netID := range ids {
		cfg, exists := t.cur.Networks[netID]
		if exists && cfg != nil {
			continue
		}
		new := &OverlayNetworkV1{}
		t.cur.Networks[netID] = new
	}
	return nil
}

// NetworkList returns NetworkID list of existing network.
func (t *OverlayNetworksV1Txn) NetworkList() (ids []driver.NetworkID) {
	for netID := range t.cur.Networks {
		ids = append(ids, netID)
	}
	return ids
}

// NetworkFromID returns network with specific NetworkID.
func (t *OverlayNetworksV1Txn) NetworkFromID(id driver.NetworkID) *OverlayNetworkV1 {
	v1, _ := t.cur.Networks[id]
	return v1
}
