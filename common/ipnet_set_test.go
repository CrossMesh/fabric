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

	ipSetFromCIDRList := func(cidrs ...string) IPNetSet {
		nets := IPNetSet{}
		for _, cidr := range cidrs {
			_, net, err := net.ParseCIDR(cidr)
			assert.NoError(t, err)
			nets = append(nets, net)
		}
		nets.Build()
		return nets
	}

	t.Run("build", func(t *testing.T) {
		nets := ipSetFromCIDRList(
			"10.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20")
		t.Log(nets)
		assert.Equal(t, 4, nets.Len())
		for i := 1; i < nets.Len(); i++ {
			assert.True(t, IPNetLess(nets[i-1], nets[i]))
		}
	})

	t.Run("equal", func(t *testing.T) {
		cidrs := []string{"10.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20"}
		nets1 := ipSetFromCIDRList(cidrs...)
		nets2 := ipSetFromCIDRList(cidrs...)
		nets3 := ipSetFromCIDRList(
			"11.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20")
		t.Log("nets1", nets1)
		t.Log("nets2", nets2)
		t.Log("nets3", nets3)
		assert.True(t, nets1.Equal(&nets2))
		assert.True(t, nets2.Equal(&nets1))
		assert.False(t, nets3.Equal(&nets1))
		assert.False(t, nets3.Equal(&nets2))
		assert.False(t, nets1.Equal(&nets3))
		assert.False(t, nets2.Equal(&nets3))
	})

	t.Run("clone", func(t *testing.T) {
		nets := ipSetFromCIDRList(
			"11.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20")
		t.Log(nets)
		cloned := nets.Clone()
		assert.Equal(t, nets, cloned)
		assert.True(t, nets.Equal(&cloned))
	})

	t.Run("merge", func(t *testing.T) {
		right := ipSetFromCIDRList(
			"10.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"172.17.0.1/20",
			"192.168.0.0/24",
			"192.168.16.0/20",
			"192.168.16.0/20")
		left := ipSetFromCIDRList(
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"172.17.0.0/19")
		t.Log("left =", left, ", right =", right)
		left.Merge(right)
		t.Log("merged =", left)
		assert.Equal(t, 6, len(left))
		for i := 1; i < left.Len(); i++ {
			assert.True(t, IPNetLess(left[i-1], left[i]))
		}
	})

	t.Run("subtract", func(t *testing.T) {
		set := ipSetFromCIDRList(
			"10.0.0.0/8",
			"172.17.0.1/20",
			"172.17.0.1/19",
			"192.168.0.0/24",
			"192.168.16.0/24",
			"192.168.16.0/20")

		var nets [3]*net.IPNet
		var err error
		_, nets[0], err = net.ParseCIDR("10.0.0.0/8")
		assert.NoError(t, err)
		_, nets[1], err = net.ParseCIDR("172.17.0.1/20")
		assert.NoError(t, err)
		_, nets[2], err = net.ParseCIDR("192.168.0.0/24")
		assert.NoError(t, err)
		t.Log(set, nets[:])
		set.Remove(nets[:]...)
		t.Log(set)

		assert.Equal(t, 3, set.Len())
		_, nets[0], err = net.ParseCIDR("172.17.0.1/19")
		assert.NoError(t, err)
		_, nets[1], err = net.ParseCIDR("192.168.16.0/24")
		assert.NoError(t, err)
		_, nets[2], err = net.ParseCIDR("192.168.16.0/20")
		assert.Contains(t, set, nets[2])
		assert.Contains(t, set, nets[1])
		assert.Contains(t, set, nets[0])
	})

	t.Run("overlap_find", func(t *testing.T) {
		var nets [3]*net.IPNet
		var err error
		_, nets[0], err = net.ParseCIDR("10.0.0.0/8")
		assert.NoError(t, err)
		_, nets[1], err = net.ParseCIDR("172.17.0.1/20")
		assert.NoError(t, err)
		_, nets[2], err = net.ParseCIDR("10.0.16.0/20")
		assert.NoError(t, err)

		overlapped, net1, net2 := IPNetOverlapped(nets[:]...)
		assert.True(t, overlapped)
		assert.Contains(t, []*net.IPNet{net1, net2}, nets[0])
		assert.Contains(t, []*net.IPNet{net1, net2}, nets[2])

		overlapped, net1, net2 = IPNetOverlapped(nets[:2]...)
		assert.False(t, overlapped)
		assert.Nil(t, net1)
		assert.Nil(t, net2)
	})

	t.Run("misc", func(t *testing.T) {
		assert.Equal(t, 23, IPMaskPrefixLen(net.IPMask{0xFF, 0xFF, 0xFE, 0x00}))
		assert.Equal(t, 17, IPMaskPrefixLen(net.IPMask{0xFF, 0xFF, 0x80, 0x00}))
		assert.Equal(t, net.IPMask{0xFF, 0xFF, 0x80, 0x00}, IPMaskFromPrefixLen(17, 4))
		assert.Equal(t, net.IPMask{0xFF, 0xFF, 0xFE, 0x00}, IPMaskFromPrefixLen(23, 4))
	})
}
