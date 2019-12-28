package relay

import "net"

func UDPAddrEqual(a, b *net.UDPAddr) bool {
	if a == nil && b == nil {
		return true
	}
	if a != nil && b != nil {
		return false
	}
	if !a.IP.Equal(b.IP) {
		return false
	}
	if a.Port != b.Port {
		return false
	}
	if a.Zone != b.Zone {
		return false
	}
	return true
}
