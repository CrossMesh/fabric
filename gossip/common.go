package gossip

import (
	"encoding/binary"
	"net"

	"github.com/crossmesh/fabric/common"
)

var ()

// IPNetSetEncode trys to marshal IPNetSet to bytes.
func IPNetSetEncode(s common.IPNetSet) ([]byte, error) {
	bins := []byte(nil)
	for _, subnet := range s {
		if subnet == nil {
			continue
		}

		ipLen := len(subnet.IP)
		if ipLen > 0xFF {
			return nil, ErrIPTooLong
		}
		bins = append(bins, byte(ipLen))

		var encodedPrefixLen [2]byte
		prefixLen := common.IPMaskPrefixLen(subnet.Mask)
		if prefixLen > 0xFFFF {
			return nil, ErrIPMaskTooLong
		}
		ipLen = prefixLen >> 3
		if prefixLen&7 > 0 {
			ipLen++
		}
		if len(subnet.IP) < ipLen {
			return nil, ErrBrokenIPNet
		}

		binary.BigEndian.PutUint16(encodedPrefixLen[:], uint16(prefixLen))
		bins = append(bins, encodedPrefixLen[:]...)
		for i := 0; i < ipLen; i++ {
			bins = append(bins, byte(subnet.IP[i]&subnet.Mask[i]))
		}
	}
	return bins, nil
}

// IPNetSetDecode trys to unmarshal structure from bytes.
func IPNetSetDecode(x []byte) (subnets common.IPNetSet, err error) {
	subnets = make(common.IPNetSet, 0)

	for i := 0; i+2 < len(x); {
		ipLen := x[i]
		prefixLen := binary.BigEndian.Uint16(x[i+1 : i+3])
		i += 3

		compressedLen := prefixLen >> 3
		if prefixLen&7 > 0 {
			if compressedLen+1 < compressedLen {
				return nil, ErrBrokenIPNetBinary
			}
			compressedLen++
		}
		cidrInfoEnd := i + int(compressedLen)
		if cidrInfoEnd > len(x) {
			return nil, ErrBrokenIPNetBinary
		}
		subnet := &net.IPNet{
			IP:   make(net.IP, ipLen),
			Mask: common.IPMaskFromPrefixLen(uint(prefixLen), uint(ipLen)),
		}
		copy(subnet.IP[:compressedLen], x[i:cidrInfoEnd])

		subnets = append(subnets, subnet)

		i = cidrInfoEnd
	}

	if len(subnets) > 1 {
		subnets.Build() // for safety.
	}

	return
}
