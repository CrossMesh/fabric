package common

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPNetSet(t *testing.T) {
	t.Run("less", func(t *testing.T) {
		var err error
		var net1, net2, net3 *net.IPNet

		_, net1, err = net.ParseCIDR("192.168.16.0/20")
		assert.NoError(t, err)
		_, net2, err = net.ParseCIDR("192.168.16.0/24")
		assert.NoError(t, err)
		_, net3, err = net.ParseCIDR("192.168.0.0/24")
		assert.NoError(t, err)

		assert.True(t, IPNetLess(net1, net2))
		assert.False(t, IPNetLess(net2, net1))

		assert.False(t, IPNetLess(net2, net3))
		assert.True(t, IPNetLess(net3, net2))

		assert.True(t, IPNetLess(net1, net3))
		assert.False(t, IPNetLess(net3, net1))

		assert.False(t, IPNetLess(net1, net1))
		assert.False(t, IPNetLess(net2, net2))
		assert.False(t, IPNetLess(net3, net3))
	})

	t.Run("build", func(t *testing.T) {
		cidrs := []string{
			"10.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20",
		}
		nets := IPNetSet{}
		for _, cidr := range cidrs {
			_, net, err := net.ParseCIDR(cidr)
			assert.NoError(t, err)
			nets = append(nets, net)
		}
		nets.Build()
		assert.Equal(t, 5, nets.Len())
		for i := 1; i < nets.Len(); i++ {
			assert.True(t, IPNetLess(nets[i-1], nets[i]))
		}
	})
}
