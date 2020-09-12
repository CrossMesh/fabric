package gossip

import (
	"net"
	"testing"

	"github.com/crossmesh/fabric/common"
	"github.com/stretchr/testify/assert"
)

func TestCommon(t *testing.T) {
	ipSetFromCIDRList := func(cidrs ...string) common.IPNetSet {
		nets := common.IPNetSet{}
		for _, cidr := range cidrs {
			_, net, err := net.ParseCIDR(cidr)
			assert.NoError(t, err)
			nets = append(nets, net)
		}
		nets.Build()
		return nets
	}

	t.Run("ipset_coding", func(t *testing.T) {
		nets := ipSetFromCIDRList(
			"10.0.0.0/8",
			"10.1.0.0/8",
			"10.0.0.0/8",
			"192.168.16.0/20",
			"192.168.16.0/24",
			"192.168.0.0/24",
			"192.168.16.0/20")

		var decoded common.IPNetSet
		t.Log("origin =", nets)
		bins, err := IPNetSetEncode(nets)
		assert.NoError(t, err)
		t.Log("encoded =", bins)
		decoded, err = IPNetSetDecode(bins)
		assert.NoError(t, err)
		t.Log("decoded =", decoded)
		assert.True(t, decoded.Equal(&nets))
	})
}
