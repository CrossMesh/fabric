package gossip

import (
	"testing"

	"github.com/crossmesh/sladder"
	"github.com/stretchr/testify/assert"
)

type FakedOverlayNetworkParamValidator struct {
	stringOnFail map[string]struct{}
}

func (v FakedOverlayNetworkParamValidator) Sync(local *string, remote string, isConcurrent bool) (changed bool, err error) {
	if local == nil {
		return false, nil
	}
	l := &sladder.KeyValue{Value: *local}
	changed, err = sladder.StringValidator{}.SyncEx(l, &sladder.KeyValue{Value: remote}, &MockMergingProps{concurrent: isConcurrent})
	if err == nil && changed {
		*local = l.Value
	}
	return
}

func (v FakedOverlayNetworkParamValidator) Validate(s string) bool {
	f := v.stringOnFail
	if f == nil {
		return true
	}
	_, fail := f[s]
	if !fail {
		return sladder.StringValidator{}.Validate(sladder.KeyValue{Value: s})
	}
	return fail
}

func (v FakedOverlayNetworkParamValidator) Txn(o string) (sladder.KVTransaction, error) {
	return sladder.StringValidator{}.Txn(sladder.KeyValue{Key: "", Value: o})
}

func TestOverlay(t *testing.T) {
	t.Run("types", func(t *testing.T) {
		t.Run("overlay_network_v1", func(t *testing.T) {
			v11, v12, v13 := OverlayNetworkV1{
				Params: "ddd",
			}, OverlayNetworkV1{
				Params: "ddc",
			}, OverlayNetworkV1{
				Params: "ddd",
			}
			assert.True(t, v11.Equal(&v13))
			assert.False(t, v11.Equal(&v12))
			assert.False(t, v12.Equal(&v13))
			assert.True(t, v12.Equal(&v12))
			assert.False(t, v12.Equal(nil))
		})

		t.Run("overlay_networks_v1", func(t *testing.T) {
			v11 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			v12 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			assert.True(t, v11.Equal(&v12))
			assert.True(t, v12.Equal(&v11))

			// compare
			v11.Networks[NetworkID{
				ID:         1,
				DriverType: CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{}
			v12.Networks[NetworkID{
				ID:         1,
				DriverType: CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{}
			assert.True(t, v11.Equal(&v12))

			v12.Networks[NetworkID{
				ID:         2,
				DriverType: CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{}
			assert.False(t, v11.Equal(&v12))

			v11.Networks[NetworkID{
				ID:         2,
				DriverType: CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{}
			assert.True(t, v11.Equal(&v12))

			// clone
			v13 := v11.Clone()
			assert.True(t, v13.Equal(&v11))
			{
				ov, has := v13.Networks[NetworkID{
					ID:         1,
					DriverType: CrossmeshSymmetryEthernet,
				}]
				assert.True(t, has)
				ov.Params = "bbb"
			}
			assert.False(t, v13.Equal(&v11)) // is it really a deep copy?
			v13 = v11.Clone()
			v13.Networks[NetworkID{
				ID:         3,
				DriverType: VxLAN,
			}] = &OverlayNetworkV1{}
			assert.False(t, v13.Equal(&v11)) // is it really a deep copy?

			// validate.
			v14 := OverlayNetworksV1{Version: 3}
			assert.Error(t, v14.Validate())

			// encoding.
			t.Log(v11.Networks[NetworkID{ID: 1, DriverType: CrossmeshSymmetryEthernet}])
			s, err := v11.EncodeToString()
			assert.NoError(t, err)
			v15 := OverlayNetworksV1{}
			assert.NoError(t, v15.DecodeStringAndValidate(s))
			assert.True(t, v15.Equal(&v11))

			assert.NoError(t, v15.DecodeStringAndValidate(""))
		})

		t.Run("overlay_networks_validator_v1", func(t *testing.T) {
			v := OverlayNetworksValidatorV1{}
			pv := FakedOverlayNetworkParamValidator{}
			pv2 := FakedOverlayNetworkParamValidator{}

			assert.Nil(t, v.RegisterDriverType(0, &pv))
			assert.Equal(t, &pv, v.GetDriverValidator(0))
			assert.Equal(t, &pv, v.RegisterDriverType(0, &pv2))
			assert.Equal(t, &pv2, v.GetDriverValidator(0))
			assert.Equal(t, &pv2, v.RegisterDriverType(0, nil))
			assert.Nil(t, v.GetDriverValidator(0))

			assert.Nil(t, v.RegisterDriverType(0, &pv))
			assert.Nil(t, v.RegisterDriverType(1, &pv))
			assert.Nil(t, v.RegisterDriverType(2, &pv))
			v11 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			v11.Networks[NetworkID{
				ID:         1,
				DriverType: CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{Params: "ddd"}
			v11.Networks[NetworkID{
				ID:         2,
				DriverType: CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{Params: "ccc"}
			pack, err := v11.EncodeToString()
			assert.NoError(t, err)

			// normal
			local := &sladder.KeyValue{Key: "k"}
			changed := false
			changed, err = v.Sync(local, &sladder.KeyValue{Key: "k", Value: pack})
			assert.NoError(t, err)
			assert.True(t, changed)
			{
				assert.True(t, v.Validate(*local))
				res := &OverlayNetworksV1{}
				err = res.DecodeStringAndValidate(local.Value)
				assert.NoError(t, err)
				if assert.Equal(t, 2, len(res.Networks)) {
					assert.Equal(t, "ddd", res.Networks[NetworkID{ID: 1, DriverType: CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, "ccc", res.Networks[NetworkID{ID: 2, DriverType: CrossmeshSymmetryRoute}].Params)
				}
			}
			// concurrent
			v11.Networks[NetworkID{
				ID:         3,
				DriverType: CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{Params: "ccd"}
			delete(v11.Networks, NetworkID{
				ID:         2,
				DriverType: CrossmeshSymmetryRoute,
			})
			pack, err = v11.EncodeToString()
			assert.NoError(t, err)
			changed, err = v.SyncEx(local, &sladder.KeyValue{Key: "k", Value: pack}, &MockMergingProps{concurrent: true})
			assert.NoError(t, err)
			assert.True(t, changed)
			{
				assert.True(t, v.Validate(*local))
				res := &OverlayNetworksV1{}
				err = res.DecodeStringAndValidate(local.Value)
				assert.NoError(t, err)
				if assert.Equal(t, 3, len(res.Networks)) {
					assert.Equal(t, "ddd", res.Networks[NetworkID{ID: 1, DriverType: CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, "ccc", res.Networks[NetworkID{ID: 2, DriverType: CrossmeshSymmetryRoute}].Params)
					assert.Equal(t, "ccd", res.Networks[NetworkID{ID: 3, DriverType: CrossmeshSymmetryRoute}].Params)
				}
			}
			changed, err = v.SyncEx(local, nil, &MockMergingProps{concurrent: true})
			assert.NoError(t, err)
			assert.False(t, changed)

			// replace currupted local value.
			local = &sladder.KeyValue{Key: "k", Value: "dalkdj"}
			assert.False(t, v.Validate(*local)) // should be invalid.
			changed, err = v.SyncEx(local, &sladder.KeyValue{Key: "k", Value: pack}, &MockMergingProps{concurrent: true})
			assert.NoError(t, err)
			assert.True(t, changed)
			{
				assert.True(t, v.Validate(*local))
				res := &OverlayNetworksV1{}
				err = res.DecodeStringAndValidate(local.Value)
				assert.NoError(t, err)
				if assert.Equal(t, 2, len(res.Networks)) {
					assert.Equal(t, "ddd", res.Networks[NetworkID{ID: 1, DriverType: CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, "ccd", res.Networks[NetworkID{ID: 3, DriverType: CrossmeshSymmetryRoute}].Params)
				}
			}
		})

		t.Run("overlay_networks_v1_txn", func(t *testing.T) {
			v := OverlayNetworksValidatorV1{}
			pv := FakedOverlayNetworkParamValidator{}
			assert.Nil(t, v.RegisterDriverType(0, &pv))
			assert.Nil(t, v.RegisterDriverType(1, &pv))
			assert.Nil(t, v.RegisterDriverType(2, &pv))

			local := &sladder.KeyValue{Key: "k"}
			rtx, err := v.Txn(*local)
			assert.NoError(t, err)
			assert.NotNil(t, rtx)
			txn := rtx.(*OverlayNetworksV1Txn)
			assert.False(t, txn.Updated())
			// failed to start txn for a unknown network.
			rtx, err = txn.ParamsTxn(NetworkID{ID: 1, DriverType: 1})
			assert.Error(t, err)
			err = txn.AddNetwork(NetworkID{ID: 1, DriverType: 1})
			assert.True(t, txn.Updated())
			assert.NoError(t, err)
			// NetworkList
			ids := txn.NetworkList()
			assert.Equal(t, 1, len(ids))
			// NetworkFromID
			param := txn.NetworkFromID(NetworkID{ID: 1, DriverType: 1})
			assert.NotNil(t, param)
			// failed to start a txn with unknown driver.
			err = txn.AddNetwork(NetworkID{ID: 1, DriverType: 100})
			assert.True(t, txn.Updated())
			assert.NoError(t, err)
			rtx, err = txn.ParamsTxn(NetworkID{ID: 1, DriverType: 100})
			assert.True(t, txn.Updated())
			assert.Error(t, err)
			assert.IsType(t, &ParamValidatorMissingError{}, err)
			assert.Nil(t, rtx)
			// normal txn.
			rtx, err = txn.ParamsTxn(NetworkID{ID: 1, DriverType: 1})
			assert.True(t, txn.Updated())
			assert.NoError(t, err)
			assert.IsType(t, &sladder.StringTxn{}, rtx)
			rtx2, err := txn.ParamsTxn(NetworkID{ID: 1, DriverType: 1})
			assert.Equal(t, rtx, rtx2)
			// Before()
			s := txn.Before()
			{
				res := OverlayNetworksV1{}
				err = res.DecodeStringAndValidate(s)
				assert.NoError(t, err)
				assert.Equal(t, 0, len(res.Networks))
			}
			// After()
			s = txn.After()
			{
				res := OverlayNetworksV1{}
				err = res.DecodeStringAndValidate(s)
				assert.NoError(t, err)
				assert.Equal(t, 2, len(res.Networks))
				_, hasNetwork := res.Networks[NetworkID{ID: 1, DriverType: 1}]
				assert.True(t, hasNetwork)
				_, hasNetwork = res.Networks[NetworkID{ID: 1, DriverType: 100}]
				assert.True(t, hasNetwork)
			}
			initialValue2 := s
			// RemoveNetwork.
			txn.RemoveNetwork(NetworkID{ID: 1, DriverType: 100})
			txn.RemoveNetwork(NetworkID{ID: 1, DriverType: 1})
			assert.False(t, txn.Updated())
			// SetRawValue.
			assert.NoError(t, txn.SetRawValue(s))
			assert.True(t, txn.Updated())
			ids = txn.NetworkList()
			assert.Equal(t, 2, len(ids))
			param = txn.NetworkFromID(NetworkID{ID: 1, DriverType: 1})
			assert.NotNil(t, param)
			param = txn.NetworkFromID(NetworkID{ID: 1, DriverType: 100})
			assert.NotNil(t, param)
			assert.True(t, txn.Updated())
			assert.NoError(t, txn.SetRawValue(txn.Before()))
			assert.False(t, txn.Updated())

			// transaction integrity.
			local = &sladder.KeyValue{Key: "k", Value: initialValue2}
			rtx, err = v.Txn(*local)
			assert.NoError(t, err)
			txn = rtx.(*OverlayNetworksV1Txn)
			assert.False(t, txn.Updated())
			rtx, err = txn.ParamsTxn(NetworkID{ID: 1, DriverType: 1})
			assert.False(t, txn.Updated())
			assert.NoError(t, err)
			if assert.IsType(t, &sladder.StringTxn{}, rtx) {
				s := rtx.(*sladder.StringTxn)
				s.Set("daksdj")
			}
			assert.True(t, txn.Updated())
			txn.RemoveNetwork(NetworkID{ID: 1, DriverType: 1})
			assert.NoError(t, txn.AddNetwork(NetworkID{ID: 1, DriverType: 1}))
			if assert.IsType(t, &sladder.StringTxn{}, rtx) {
				s := rtx.(*sladder.StringTxn)
				assert.Equal(t, "", s.Get())
			}
		})
	})
}
