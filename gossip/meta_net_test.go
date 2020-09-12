package gossip

import (
	"testing"

	"github.com/crossmesh/sladder"
	"github.com/stretchr/testify/assert"
)

type MockMergingProps struct {
	concurrent bool
}

func (p *MockMergingProps) Concurrent() bool         { return p.concurrent }
func (p *MockMergingProps) Get(x string) interface{} { return nil }

func TestMetaNetModel(t *testing.T) {
	t.Run("types", func(t *testing.T) {
		t.Run("network_endpoint_set", func(t *testing.T) {
			set := NetworkEndpointSetV1{
				&NetworkEndpointV1{
					Type:     1,
					Priority: 10,
					Endpoint: "10.240.0.1:3880",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:3890",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:3890",
				},
				nil,
				&NetworkEndpointV1{
					Type:     2,
					Priority: 0,
					Endpoint: "ssssss",
				},
				nil, nil,
				&NetworkEndpointV1{
					Type:     1,
					Priority: 7,
					Endpoint: "10.240.0.1:3890",
				},
				nil,
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "ssssbs",
				},
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "sskssbs",
				},
				nil,
			}
			set.Build()
			for i := 0; i < set.Len(); i++ {
				assert.NotNil(t, set[i])
			}
			t.Log("set1 =", set)

			// equal
			set2 := NetworkEndpointSetV1{
				&NetworkEndpointV1{
					Type:     1,
					Priority: 10,
					Endpoint: "10.240.0.1:3880",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 8,
					Endpoint: "10.240.0.1:3890",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 7,
					Endpoint: "10.240.0.1:3890",
				},
				nil,
				&NetworkEndpointV1{
					Type:     2,
					Priority: 0,
					Endpoint: "ssssss",
				},
				nil, nil,
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "ssssbs",
				},
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "sskssbs",
				},
				nil,
			}
			set2.Build()
			t.Log("set2 =", set2)
			assert.True(t, set.Equal(set2))

			set3 := NetworkEndpointSetV1{
				&NetworkEndpointV1{
					Type:     1,
					Priority: 10,
					Endpoint: "10.240.0.1:38800",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:3899",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:38790",
				},
			}
			set3.Build()
			assert.False(t, set3.Equal(set2))
			set3.Push((*NetworkEndpointV1)(nil))

			// clone.
			set4 := set.Clone()
			assert.True(t, set4.Equal(set))
			t.Log("cloned set =", set4)

			// merge.
			{
				ori := set.Clone()
				set.Merge(set3.Clone())
				t.Log("set merges set3 =", set)
				assert.Equal(t, 8, set.Len())
				for _, e := range ori {
					assert.Contains(t, set, e)
				}
				for _, e := range set3 {
					if e == nil {
						continue
					}
					assert.Contains(t, set, e)
				}
			}
		})

		t.Run("network_endpoints_v1_types", func(t *testing.T) {
			set := NetworkEndpointSetV1{
				&NetworkEndpointV1{
					Type:     1,
					Priority: 10,
					Endpoint: "10.240.0.1:3880",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:3890",
				},
				&NetworkEndpointV1{
					Type:     1,
					Priority: 9,
					Endpoint: "10.240.0.1:3890",
				},
				nil,
				&NetworkEndpointV1{
					Type:     2,
					Priority: 0,
					Endpoint: "ssssss",
				},
				nil, nil,
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "ssssbs",
				},
				&NetworkEndpointV1{
					Type:     3,
					Priority: 11,
					Endpoint: "sskssbs",
				},
				nil,
			}
			set.Build()
			v1 := NetworkEndpointsV1{Version: 2, Endpoints: set}
			assert.Error(t, v1.Validate())
			v1 = NetworkEndpointsV1{Version: 1, Endpoints: set}
			cloned := &NetworkEndpointsV1{Version: 1, Endpoints: set.Clone()}
			assert.True(t, v1.Equal(cloned))
			cloned = v1.Clone()
			assert.True(t, v1.Equal(cloned))

			// encode decode.
			s, err := v1.EncodeString()
			assert.NoError(t, err)
			v1Dec := &NetworkEndpointsV1{}
			assert.NoError(t, v1Dec.DecodeStringAndValidate(s))
			assert.True(t, v1Dec.Equal(&v1))
			assert.NoError(t, v1Dec.DecodeStringAndValidate(""))
		})
	})

	t.Run("gossip_sync", func(t *testing.T) {
		v := NetworkEndpointsValidatorV1{}
		set := NetworkEndpointSetV1{
			&NetworkEndpointV1{
				Type:     1,
				Priority: 10,
				Endpoint: "10.240.0.1:3880",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 9,
				Endpoint: "10.240.0.1:3890",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 9,
				Endpoint: "10.240.0.1:3898",
			},
		}
		set.Build()
		v1 := NetworkEndpointsV1{Version: 1, Endpoints: set}
		s1, err := v1.EncodeString()
		assert.NoError(t, err)

		var s2 string
		set2 := NetworkEndpointSetV1{
			&NetworkEndpointV1{
				Type:     1,
				Priority: 10,
				Endpoint: "10.240.0.1:3880",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 10,
				Endpoint: "10.240.0.1:3891",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 9,
				Endpoint: "10.240.0.1:3898",
			},
		}
		set2.Build()
		v1r := NetworkEndpointsV1{Version: 1, Endpoints: set2}
		s2, err = v1r.EncodeString()
		assert.NoError(t, err)
		var changed bool

		// validate.
		local := &sladder.KeyValue{Value: s1}
		local2 := &sladder.KeyValue{Value: s1}
		local3 := &sladder.KeyValue{Value: "ka"} // invalid value will be replaced.
		assert.True(t, v.Validate(*local))
		assert.False(t, v.Validate(*local3))

		// normal changed.
		changed, err = v.Sync(local, &sladder.KeyValue{
			Value: s2,
		})
		assert.NoError(t, err)
		assert.True(t, changed)
		changed, err = v.SyncEx(local2, &sladder.KeyValue{
			Value: s2,
		}, &MockMergingProps{concurrent: false})
		assert.NoError(t, err)
		assert.True(t, changed)
		changed, err = v.Sync(local3, &sladder.KeyValue{
			Value: s2,
		})
		assert.NoError(t, err)
		assert.True(t, changed)
		{
			res := NetworkEndpointsV1{}
			err = res.DecodeStringAndValidate(local.Value)
			assert.NoError(t, err)
			res.Equal(&v1r)
			err = res.DecodeStringAndValidate(local2.Value)
			assert.NoError(t, err)
			res.Equal(&v1r)
			err = res.DecodeStringAndValidate(local3.Value)
			assert.NoError(t, err)
			res.Equal(&v1r)
		}

		// local nil.
		local = nil
		changed, err = v.Sync(local, &sladder.KeyValue{
			Value: s2,
		})
		assert.NoError(t, err)
		assert.False(t, changed)

		// no changes.
		changed, err = v.Sync(&sladder.KeyValue{
			Value: s1,
		}, &sladder.KeyValue{
			Value: s1,
		})
		assert.NoError(t, err)
		assert.False(t, changed)

		// concurrent changes.
		local = &sladder.KeyValue{Value: s1}
		changed, err = v.SyncEx(local, &sladder.KeyValue{
			Value: s2,
		}, &MockMergingProps{concurrent: true})
		assert.NoError(t, err)
		assert.True(t, changed)
		{
			res := NetworkEndpointsV1{}
			err = res.DecodeStringAndValidate(local.Value)
			assert.NoError(t, err)
			except := NetworkEndpointsV1{Version: 1}
			except.Endpoints = v1r.Endpoints.Clone()
			except.Endpoints.Merge(v1.Endpoints)
			assert.True(t, res.Equal(&except))
		}

		// concurrent deletion.
		changed, err = v.SyncEx(&sladder.KeyValue{
			Value: s1,
		}, nil, &MockMergingProps{concurrent: true})
		assert.NoError(t, err)
		assert.False(t, changed)

		// non-concurrent deletion.
		changed, err = v.SyncEx(&sladder.KeyValue{
			Value: s1,
		}, nil, &MockMergingProps{concurrent: false})
		assert.NoError(t, err)
		assert.True(t, changed)
	})

	t.Run("txn", func(t *testing.T) {
		v := NetworkEndpointsValidatorV1{}
		set := NetworkEndpointSetV1{
			&NetworkEndpointV1{
				Type:     1,
				Priority: 10,
				Endpoint: "10.240.0.1:3880",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 10,
				Endpoint: "10.240.0.1:3891",
			},
			&NetworkEndpointV1{
				Type:     1,
				Priority: 9,
				Endpoint: "10.240.0.1:3898",
			},
		}
		set.Build()
		v1 := NetworkEndpointsV1{Version: 1, Endpoints: set.Clone()}
		s, err := v1.EncodeString()
		assert.NoError(t, err)

		var rtx sladder.KVTransaction
		rtx, err = v.Txn(sladder.KeyValue{Value: s})
		assert.NoError(t, err)
		txn := rtx.(*NetworkEndpointsV1Txn)
		assert.False(t, txn.Updated())
		assert.Equal(t, s, txn.Before())

		assert.True(t, txn.AddEndpoints(NetworkEndpointV1{
			Type:     1,
			Priority: 10,
			Endpoint: "10.240.0.1:4880",
		}))
		assert.True(t, txn.Updated())
		txn.UpdateEndpoints(set...)
		assert.False(t, txn.Updated())

		assert.True(t, txn.AddEndpoints(NetworkEndpointV1{
			Type:     1,
			Priority: 10,
			Endpoint: "10.240.0.1:4880",
		}))
		assert.True(t, txn.Updated())
		assert.NoError(t, txn.SetRawValue(s))
		assert.False(t, txn.Updated())

		sr := txn.After()
		res := &NetworkEndpointsV1{}
		assert.NoError(t, res.DecodeStringAndValidate(sr))
		assert.True(t, res.Equal(&v1))
	})
}
