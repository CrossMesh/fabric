package gossip

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/crossmesh/sladder"
)

var (
	ErrIPTooLong         = errors.New("IP is too long")
	ErrIPMaskTooLong     = errors.New("IPMask is too long")
	ErrBrokenIPNet       = errors.New("IPNet structure is broken")
	ErrBrokenIPNetBinary = errors.New("IPNet binary stream is broken")

	ErrParamValidatorMissing = errors.New("parameter validator of overlay network ")
)

// ParamValidatorMissingError will be raised when no related parameter validator is found for a overlay network.
type ParamValidatorMissingError struct {
	want OverlayDriverType
}

func (e *ParamValidatorMissingError) Error() string {
	return fmt.Sprintf("wants parameter validator for overlay network type %s", e.want)
}

type NetworkNotFoundError struct {
	want NetworkID
}

func (e *NetworkNotFoundError) Error() string {
	return fmt.Sprintf("network %v not found", e.want.String())
}

// OverlayDriverType is global ovaley network driver ID.
type OverlayDriverType uint16

const (
	UnknownOverlayDriver      = OverlayDriverType(0)
	CrossmeshSymmetryEthernet = OverlayDriverType(1)
	CrossmeshSymmetryRoute    = OverlayDriverType(2)
	VxLAN                     = OverlayDriverType(3)
)

func (t OverlayDriverType) String() string {
	switch t {
	case CrossmeshSymmetryEthernet:
		return "crossmesh_sym_eth"
	case CrossmeshSymmetryRoute:
		return "crossmesh_sym_route"
	case VxLAN:
		return "vxlan"
	}
	return "unknown"
}

// NetworkID is overlay network identifier.
type NetworkID struct {
	ID         int32             `json:"id,omitempty"`
	DriverType OverlayDriverType `json:"drv,omitempty"`
}

func (id *NetworkID) String() string {
	return id.DriverType.String() + "/" + strconv.FormatInt(int64(id.ID), 10)
}

const (
	// VersionOverlayNetworksV1 is version value of VersionOverlayNetworksV1 data model.
	VersionOverlayNetworksV1 = uint16(1)
)

// OverlayNetworkV1 contains joined overlay networks of peer.
type OverlayNetworkV1 struct {
	Params string `json:"p,omitempty"`
}

// Equal checks whether fields in two OverlayNetworkV1 are equal.
func (v1 *OverlayNetworkV1) Equal(x *OverlayNetworkV1) bool {
	if v1 == x {
		return true
	}
	if v1 == nil || x == nil {
		return false
	}
	return v1.Params == x.Params
}

// OverlayNetworkParamValidator implements network parameter data model for specific OverlayDriverType
type OverlayNetworkParamValidator interface {
	Sync(local *string, remote string, isConcurrent bool) (bool, error)
	Validate(string) bool
	Txn(string) (sladder.KVTransaction, error)
}

type packOverlayNetworkV1 struct {
	OverlayNetworkV1
	NetworkID
}

// OverlayNetworksV1 contains joined overlay networks of peer.
type OverlayNetworksV1 struct {
	Version  uint16                          `json:"v,omitempty"`
	Networks map[NetworkID]*OverlayNetworkV1 `json:"nets,omitempty"`
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
		new.Networks = map[NetworkID]*OverlayNetworkV1{}
		for netID, cfg := range v1.Networks {
			o := &OverlayNetworkV1{}
			new.Networks[netID] = o
			*o = *cfg
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
	Version  uint16                 `json:"v,omitempty"`
	Networks []packOverlayNetworkV1 `json:"nets,omitempty"`
}

// EncodeToString trys to marshal structure to string.
func (v1 *OverlayNetworksV1) EncodeToString() (string, error) {
	pack := packOverlayNetworksV1{Version: VersionOverlayNetworksV1}
	pack.Networks = make([]packOverlayNetworkV1, 0, len(v1.Networks))
	for netID, cfg := range v1.Networks {
		pack.Networks = append(pack.Networks, packOverlayNetworkV1{
			OverlayNetworkV1: OverlayNetworkV1{
				Params: cfg.Params,
			},
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
	netMap := make(map[NetworkID]*OverlayNetworkV1, len(pack.Networks))
	for _, net := range pack.Networks {
		if _, dup := netMap[net.NetworkID]; dup {
			return fmt.Errorf("OverlayNetworksV1 has broken integrity. Duplicated network ID %v found", net.NetworkID)
		}
		o := &OverlayNetworkV1{}
		netMap[net.NetworkID] = o
		*o = net.OverlayNetworkV1
	}
	v1.Version = pack.Version
	v1.Networks = netMap
	return nil
}

// Validate validates fields.
func (v1 *OverlayNetworksV1) Validate() error {
	if actual := v1.Version; actual != VersionOverlayNetworksV1 {
		return &ModelVersionUnmatchedError{Name: "OverlayNetworksV1", Actual: actual, Expected: VersionOverlayNetworksV1}
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
	lock            sync.RWMutex
	paramValidators map[OverlayDriverType]OverlayNetworkParamValidator
}

// RegisterDriverType registers valid driver type.
func (v1 *OverlayNetworksValidatorV1) RegisterDriverType(driverType OverlayDriverType,
	validator OverlayNetworkParamValidator) (old OverlayNetworkParamValidator) {
	v1.lock.Lock()
	defer v1.lock.Unlock()

	if v1.paramValidators == nil {
		if validator == nil {
			return nil
		}
		v1.paramValidators = make(map[OverlayDriverType]OverlayNetworkParamValidator)
	}

	old, _ = v1.paramValidators[driverType]
	if validator == nil {
		delete(v1.paramValidators, driverType)
	} else {
		v1.paramValidators[driverType] = validator
	}

	return
}

// GetDriverValidator gets validator of given driver type.
func (v1 *OverlayNetworksValidatorV1) GetDriverValidator(driverType OverlayDriverType) (validator OverlayNetworkParamValidator) {
	v1.lock.RLock()
	defer v1.lock.RUnlock()

	if v1.paramValidators == nil {
		return nil
	}
	validator, _ = v1.paramValidators[driverType]
	return
}

// Sync merges state of OverlayNetworksV1 to local.
func (v1 *OverlayNetworksValidatorV1) Sync(local, remote *sladder.KeyValue) (changed bool, err error) {
	return v1.sync(local, remote, false)
}

func (v1 *OverlayNetworksValidatorV1) sync(local, remote *sladder.KeyValue, isConcurrent bool) (changed bool, err error) {
	if remote == nil { // Deletion.
		return true, nil
	}
	lv, rv := OverlayNetworksV1{}, OverlayNetworksV1{}
	if changed, err = v1.presync(local, remote, &lv, &rv); changed || err != nil {
		return
	}

	changed = false

	newNetMap := make(map[NetworkID]*OverlayNetworkV1)
	if isConcurrent {
		// merge.
		for netID, net := range lv.Networks {
			newNetMap[netID] = &OverlayNetworkV1{
				Params: net.Params,
			}
		}
	}
	for netID, net := range rv.Networks { // update.
		validator := v1.GetDriverValidator(netID.DriverType)
		if validator == nil {
			return false, &ParamValidatorMissingError{want: netID.DriverType}
		}

		oldCfg, hasOld := lv.Networks[netID]
		localString := ""
		if hasOld {
			// existing.
			newNetMap[netID] = oldCfg
			localString = oldCfg.Params
		}
		if cfgChanged, ierr := validator.Sync(&localString, net.Params, isConcurrent); ierr != nil {
			return false, ierr
		} else if cfgChanged {
			newNetMap[netID] = &OverlayNetworkV1{
				Params: localString,
			}
			changed = true
		}
	}
	lv.Networks = newNetMap
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
	for netID, net := range on1.Networks {
		validator := v1.GetDriverValidator(netID.DriverType)
		if validator == nil {
			return false
		}
		if !validator.Validate(net.Params) {
			return false
		}
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

	oldRaw    string
	old, cur  *OverlayNetworksV1
	paramTxns map[NetworkID]sladder.KVTransaction
}

// CloneCurrent makes deepcopy of current OverlayNetworksV1 structure.
func (t *OverlayNetworksV1Txn) CloneCurrent() *OverlayNetworksV1 { return t.cur.Clone() }

// Txn starts KVTransaction of OverlayNetworksV1.
func (v1 *OverlayNetworksValidatorV1) Txn(kv sladder.KeyValue) (sladder.KVTransaction, error) {
	txn := &OverlayNetworksV1Txn{
		oldRaw:    kv.Value,
		validator: v1,
		paramTxns: make(map[NetworkID]sladder.KVTransaction),
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
	for netID, txn := range t.paramTxns { // update txns.
		packParam := ""
		cfg, hasCfg := t.cur.Networks[netID]
		if hasCfg && cfg != nil {
			packParam = cfg.Params
		}
		txn.SetRawValue(packParam)
	}

	return nil
}

// Before returns origin raw value.
func (t *OverlayNetworksV1Txn) Before() string { return t.oldRaw }

// After return current raw value.
func (t *OverlayNetworksV1Txn) After() string {
	cur := t.cur.Clone()
	for netID, cfg := range cur.Networks {
		txn, hasTxn := t.paramTxns[netID]
		if !hasTxn {
			continue
		}
		cfg.Params = txn.After()
	}
	s, err := cur.EncodeToString()
	if err != nil {
		panic(err) // should not happen.
	}
	return s
}

// Updated checks whether value is updated.
func (t *OverlayNetworksV1Txn) Updated() bool {
	if t.old == t.cur {
		return t.hasParamsUpdated()
	}
	if !t.old.Equal(t.cur) {
		return true
	}
	return t.hasParamsUpdated()
}

func (t *OverlayNetworksV1Txn) hasParamsUpdated() bool {
	for netID := range t.cur.Networks {
		txn, hasTxn := t.paramTxns[netID]
		if !hasTxn {
			continue
		}
		if txn.Updated() {
			return true
		}
	}
	return false
}

// Version returns current value of version field in OverlayNetworksV1.
func (t *OverlayNetworksV1Txn) Version() uint16 { return t.cur.Version }

// RemoveNetwork removes networks according to a set of NetworkID.
func (t *OverlayNetworksV1Txn) RemoveNetwork(ids ...NetworkID) {
	if len(ids) < 1 {
		return
	}
	t.copyOnWrite()
	for _, netID := range ids {
		delete(t.cur.Networks, netID)
	}
}

// AddNetwork adds networks according to a set of NetworkID.
func (t *OverlayNetworksV1Txn) AddNetwork(ids ...NetworkID) (err error) {
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

		if rtx, hasTransaction := t.paramTxns[netID]; hasTransaction {
			if err = rtx.SetRawValue(new.Params); err != nil {
				return
			}
		}
	}
	return nil
}

// NetworkList returns NetworkID list of existing network.
func (t *OverlayNetworksV1Txn) NetworkList() (ids []NetworkID) {
	for netID := range t.cur.Networks {
		ids = append(ids, netID)
	}
	return ids
}

// NetworkFromID returns network with specific NetworkID.
func (t *OverlayNetworksV1Txn) NetworkFromID(id NetworkID) *OverlayNetworkV1 {
	v1, _ := t.cur.Networks[id]
	return v1
}

// ParamsTxn creates get KVTransaction for network params.
func (t *OverlayNetworksV1Txn) ParamsTxn(id NetworkID) (sladder.KVTransaction, error) {
	cfg, hasNetwork := t.cur.Networks[id]
	if !hasNetwork {
		return nil, &NetworkNotFoundError{want: id}
	}
	rtx, hasTransaction := t.paramTxns[id]
	if hasTransaction {
		return rtx, nil
	}
	cfgValidator := t.validator.GetDriverValidator(id.DriverType)
	if cfgValidator == nil {
		return nil, &ParamValidatorMissingError{want: id.DriverType}
	}
	rtx, err := cfgValidator.Txn(cfg.Params)
	if err != nil {
		return nil, err
	}
	t.paramTxns[id] = rtx
	return rtx, nil
}
