package gossip

import (
	"testing"

	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/sladder"
	"github.com/stretchr/testify/assert"
)

type MockMergingProps struct {
	concurrent bool
}

func (p *MockMergingProps) Concurrent() bool         { return p.concurrent }
func (p *MockMergingProps) Get(x string) interface{} { return nil }

func TestOverlay(t *testing.T) {
	t.Run("types", func(t *testing.T) {
		t.Run("overlay_network_v1", func(t *testing.T) {
			v11, v12, v13 := OverlayNetworkV1{
				Params: map[string][]byte{
					"k": {0, 0, 1},
					"d": {0, 1, 1},
					"c": {88, 0, 1},
				},
			}, OverlayNetworkV1{
				Params: map[string][]byte{
					"k": {0, 0, 1},
					"p": {0, 1, 1},
					"c": {88, 0, 1},
				},
			}, OverlayNetworkV1{
				Params: map[string][]byte{
					"k": {0, 0, 1},
					"d": {0, 1, 1},
					"c": {88, 0, 1},
				},
			}
			assert.True(t, v11.Equal(&v13))
			assert.False(t, v11.Equal(&v12))
			assert.False(t, v12.Equal(&v13))
			assert.True(t, v12.Equal(&v12))
			assert.False(t, v12.Equal(nil))
			v13.Params["c"] = []byte{0, 0, 2}
			assert.False(t, v11.Equal(&v13))
		})

		t.Run("overlay_networks_v1", func(t *testing.T) {
			v11 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			v12 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			assert.True(t, v11.Equal(&v12))
			assert.True(t, v12.Equal(&v11))

			// compare
			v11.Networks[NetworkID{
				ID:         1,
				DriverType: driver.CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{}
			v12.Networks[NetworkID{
				ID:         1,
				DriverType: driver.CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{}
			assert.True(t, v11.Equal(&v12))

			v12.Networks[NetworkID{
				ID:         2,
				DriverType: driver.CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{}
			assert.False(t, v11.Equal(&v12))

			v11.Networks[NetworkID{
				ID:         2,
				DriverType: driver.CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{}
			assert.True(t, v11.Equal(&v12))

			// clone
			v13 := v11.Clone()
			assert.True(t, v13.Equal(&v11))
			{
				ov, has := v13.Networks[NetworkID{
					ID:         1,
					DriverType: driver.CrossmeshSymmetryEthernet,
				}]
				assert.True(t, has)
				ov.Params = map[string][]byte{
					"k": {0, 0},
				}
			}
			assert.False(t, v13.Equal(&v11)) // is it really a deep copy?
			v13 = v11.Clone()
			v13.Networks[NetworkID{
				ID:         3,
				DriverType: driver.VxLAN,
			}] = &OverlayNetworkV1{}
			assert.False(t, v13.Equal(&v11)) // is it really a deep copy?

			// validate.
			v14 := OverlayNetworksV1{Version: 3}
			assert.Error(t, v14.Validate())

			// encoding.
			t.Log(v11.Networks[NetworkID{ID: 1, DriverType: driver.CrossmeshSymmetryEthernet}])
			s, err := v11.EncodeToString()
			assert.NoError(t, err)
			v15 := OverlayNetworksV1{}
			assert.NoError(t, v15.DecodeStringAndValidate(s))
			assert.True(t, v15.Equal(&v11))

			assert.NoError(t, v15.DecodeStringAndValidate(""))
		})

		t.Run("overlay_networks_validator_v1", func(t *testing.T) {
			v := OverlayNetworksValidatorV1{}
			p1, p2 := map[string][]byte{"k": {3, 3, 3}}, map[string][]byte{"k": {3, 3, 4}}
			v11 := OverlayNetworksV1{Version: 1, Networks: make(map[NetworkID]*OverlayNetworkV1)}
			v11.Networks[NetworkID{
				ID:         1,
				DriverType: driver.CrossmeshSymmetryEthernet,
			}] = &OverlayNetworkV1{Params: p1}
			v11.Networks[NetworkID{
				ID:         2,
				DriverType: driver.CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{Params: p2}
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
					assert.Equal(t, p1, res.Networks[NetworkID{ID: 1, DriverType: driver.CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, p2, res.Networks[NetworkID{ID: 2, DriverType: driver.CrossmeshSymmetryRoute}].Params)
				}
			}
			// concurrent
			p3 := map[string][]byte{
				"k": {3, 3, 5},
				"p": {4, 5},
			}
			v11.Networks[NetworkID{
				ID:         3,
				DriverType: driver.CrossmeshSymmetryRoute,
			}] = &OverlayNetworkV1{Params: p3}
			delete(v11.Networks, NetworkID{
				ID:         2,
				DriverType: driver.CrossmeshSymmetryRoute,
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
					assert.Equal(t, p1, res.Networks[NetworkID{ID: 1, DriverType: driver.CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, p2, res.Networks[NetworkID{ID: 2, DriverType: driver.CrossmeshSymmetryRoute}].Params)
					assert.Equal(t, p3, res.Networks[NetworkID{ID: 3, DriverType: driver.CrossmeshSymmetryRoute}].Params)
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
					assert.Equal(t, p1, res.Networks[NetworkID{ID: 1, DriverType: driver.CrossmeshSymmetryEthernet}].Params)
					assert.Equal(t, p2, res.Networks[NetworkID{ID: 3, DriverType: driver.CrossmeshSymmetryRoute}].Params)
				}
			}
		})

		t.Run("overlay_networks_v1_txn", func(t *testing.T) {
			v := OverlayNetworksValidatorV1{}

			local := &sladder.KeyValue{Key: "k"}
			rtx, err := v.Txn(*local)
			assert.NoError(t, err)
			assert.NotNil(t, rtx)
			txn := rtx.(*OverlayNetworksV1Txn)
			assert.False(t, txn.Updated())
			// NetworkList
			txn.AddNetwork(NetworkID{ID: 1, DriverType: 1})
			ids := txn.NetworkList()
			assert.Equal(t, 1, len(ids))
			// NetworkFromID
			param := txn.NetworkFromID(NetworkID{ID: 1, DriverType: 1})
			assert.NotNil(t, param)
			txn.AddNetwork(NetworkID{ID: 1, DriverType: 100})
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
		})
	})
}
