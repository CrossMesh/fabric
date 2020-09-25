package gossip

import (
	"net"
	"testing"

	"github.com/crossmesh/fabric/common"
	"github.com/stretchr/testify/assert"
)

func TestCrossmeshOverlay(t *testing.T) {
	t.Run("types", func(t *testing.T) {
		v1 := CrossmeshOverlayParamV1{}

		subnets := common.IPNetSet(nil)
		_, subnet, err := net.ParseCIDR("172.17.0.0/24")
		assert.NoError(t, err)
		subnets = append(subnets, subnet)
		_, subnet, err = net.ParseCIDR("172.17.1.0/24")
		assert.NoError(t, err)
		subnets = append(subnets, subnet)
		_, subnet, err = net.ParseCIDR("172.17.2.0/24")
		assert.NoError(t, err)
		subnets = append(subnets, subnet)
		v1.Subnets.Merge(subnets)

		// Equal()
		v12 := CrossmeshOverlayParamV1{}
		v12.Subnets.Merge(subnets)
		assert.True(t, v12.Equal(&v1))
		// Clone()
		assert.True(t, v12.Equal(v1.Clone()))

		// encoding.
		bin, err := v1.Encode()
		assert.NoError(t, err)
		v13 := CrossmeshOverlayParamV1{}
		assert.NoError(t, v13.Decode(bin))
		assert.True(t, v13.Equal(&v1))
	})

	t.Run("validator", func(t *testing.T) {
		v := CrossmeshOverlayParamV1Validator{}

		var subnets [4]*net.IPNet
		var err error
		_, subnets[0], err = net.ParseCIDR("172.17.0.0/24")
		assert.NoError(t, err)
		_, subnets[1], err = net.ParseCIDR("172.17.1.0/24")
		assert.NoError(t, err)
		_, subnets[2], err = net.ParseCIDR("172.17.2.0/24")
		assert.NoError(t, err)
		_, subnets[3], err = net.ParseCIDR("172.17.3.0/24")
		assert.NoError(t, err)

		set := common.IPNetSet(nil)
		set = append(set, subnets[:3]...)
		set.Build()

		v1 := CrossmeshOverlayParamV1{}
		v1.Subnets.Merge(set)
		bin, err := v1.Encode()
		assert.NoError(t, err)
		s1 := string(bin)

		// Validate()
		assert.True(t, v.Validate(s1))
		assert.True(t, v.Validate(""))
		assert.False(t, v.Validate("dadskj"))

		// normal sync.
		local, changed := "dkj", false
		changed, err = v.Sync(&local, s1, false)
		assert.True(t, changed)
		assert.NoError(t, err)
		local, changed = "", false
		changed, err = v.Sync(&local, s1, false)
		assert.NoError(t, err)
		assert.True(t, changed)
		changed, err = v.Sync(nil, s1, false)
		assert.NoError(t, err)
		assert.False(t, changed)
		// concurrent sync.
		set = common.IPNetSet{}
		set = append(set, subnets[1:]...)
		set.Build()
		v1 = CrossmeshOverlayParamV1{}
		v1.Subnets.Merge(set)
		bin, err = v1.Encode()
		assert.NoError(t, err)
		s2 := string(bin)
		changed, err = v.Sync(&local, s2, true)
		assert.NoError(t, err)
		assert.True(t, changed)
		{
			res := CrossmeshOverlayParamV1{}
			assert.NoError(t, res.Decode([]byte(local)))
			assert.Equal(t, 4, len(res.Subnets))
		}
		changed, err = v.Sync(&local, s2, true)
		assert.NoError(t, err)
		assert.False(t, changed)
		t.Log(local)

		// txn.
		rtx, err := v.Txn(local)
		assert.NoError(t, err)
		assert.IsType(t, &CrossmeshOverlayParamV1Txn{}, rtx)
		txn := rtx.(*CrossmeshOverlayParamV1Txn)
		assert.False(t, txn.Updated())
		_, subnet, err := net.ParseCIDR("192.168.0.1/24")
		assert.True(t, txn.AddSubnet(subnet))
		assert.NoError(t, err)
		assert.True(t, txn.Updated())
		assert.True(t, txn.RemoveSubnet(subnets[2]))
		assert.NoError(t, txn.SetRawValue(local))
		assert.False(t, txn.Updated())
		assert.True(t, txn.RemoveSubnet(subnets[2]))
		assert.True(t, txn.Updated())
		assert.NoError(t, txn.SetRawValue(txn.Before()))
		assert.False(t, txn.Updated())
		{
			s := txn.After()
			res := CrossmeshOverlayParamV1{}
			assert.NoError(t, res.Decode([]byte(s)))
			expect := CrossmeshOverlayParamV1{}
			assert.NoError(t, expect.Decode([]byte(local)))
			assert.True(t, expect.Equal(&res))
		}
	})
}
