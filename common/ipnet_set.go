package common

import (
	"encoding/binary"
	"net"
	"sort"
)

// IPNetLess is less comparator for net.IPNet.
func IPNetLess(n1, n2 *net.IPNet) bool {
	if n1 == n2 {
		return false
	}
	if n2 == nil {
		return true
	} else if n1 == nil {
		return false
	}

	ipLen := len(n1.IP)
	if len2 := len(n2.IP); ipLen != len2 {
		return ipLen < len2
	}
	for idx := 0; idx < ipLen; idx++ {
		m1, m2 := n1.Mask[idx], n2.Mask[idx]
		if m1 != m2 {
			return m1 < m2 // network i is larger?
		}
		if m1 == 0 {
			break // fast path: mask must already be equal.
		}
	}
	for idx := 0; idx < ipLen; idx++ {
		m1, m2 := n1.Mask[idx], n2.Mask[idx]
		ip1, ip2 := n1.IP[idx]&m1, n2.IP[idx]&m2
		if ip1 != ip2 {
			return ip1 < ip2 // just simply sort by IP.
		}
	}
	return false
}

// IPNetSet contains a sorted set of IPNet.
type IPNetSet []*net.IPNet

// Len is the number of elements in IPNet collection.
func (s IPNetSet) Len() int { return len(s) }

// Less reports whether the element wieh index i should sort before the element with index j.
func (s IPNetSet) Less(i, j int) bool { return IPNetLess(s[i], s[j]) }

// Swap swaps the elements with indexes i and j.
func (s IPNetSet) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Pop remove `sz` elements from the tail.
func (s *IPNetSet) Pop(sz int) {
	oldLen := len(*s)
	*s = (*s)[:oldLen-sz]
}

// Push append new element `x` to the tail.
func (s *IPNetSet) Push(x interface{}) { *s = append(*s, x.(*net.IPNet)) }

// Elem returns the element with index `i`.
func (s IPNetSet) Elem(i int) interface{} { return s[i] }

// Build rebuilds IPNetSet.
func (s *IPNetSet) Build() {
	SortedSetBuild(s)
	validEnd := sort.Search(len(*s), func(i int) bool { return i >= len(*s) || (*s)[i] == nil })
	*s = (*s)[:validEnd]
}

// Clone makes a deep copy.
func (s IPNetSet) Clone() (new IPNetSet) {
	new = make(IPNetSet, len(s))
	for idx := range s {
		old := s[idx]
		net := &net.IPNet{}
		net.IP = append(net.IP, old.IP...)
		net.Mask = append(net.Mask, old.Mask...)
		new[idx] = net
	}
	return
}

// Equal checks whether two IPNetSet are equal.
func (s *IPNetSet) Equal(s2 *IPNetSet) bool {
	if s == s2 {
		return true
	}
	if len(*s) != len(*s2) {
		return false
	}
	if len(*s) < 1 {
		return true
	}
	for i := 0; i < len(*s); i++ {
		if IPNetLess((*s)[i], (*s2)[i]) != IPNetLess((*s2)[i], (*s)[i]) {
			return false
		}
	}
	return true
}

// Merge merges IPNetSet.
func (s *IPNetSet) Merge(src IPNetSet) bool { return SortedSetMerge(s, &src) }

// Remove removes networks from set.
func (s *IPNetSet) Remove(nets ...*net.IPNet) bool {
	r := IPNetSet(nets)
	r = r.Clone()
	r.Build()

	return SortedSetSubstract(s, &r, func(x, y interface{}) bool {
		return IPNetLess(x.(*net.IPNet), y.(*net.IPNet))
	})
}

// FindOverlapped starts at index `start` to finds first two overlapped IPNet.
// It returns the index of the first overlapped and references to IPNet pair.
// In case of no overlapped IPNet, it returns index s.Len() and nil references.
func (s IPNetSet) FindOverlapped(start int) (index int, n1 *net.IPNet, n2 *net.IPNet) {
	if start < 1 {
		start = 0
	}

	start++
	for start < s.Len() {
		if s[start-1].Contains(s[start].IP) {
			return start - 1, s[start-1], s[start]
		}
		start++
	}
	return s.Len(), nil, nil
}

// IPNetOverlapped checks whether a set of IPNet have any overlapped part.
// It returns the overlapped network pair that it first finds.
func IPNetOverlapped(nets ...*net.IPNet) (bool, *net.IPNet, *net.IPNet) {
	if len(nets) < 2 {
		return false, nil, nil
	}
	set := IPNetSet(nets)
	set = set.Clone()
	set.Build()
	_, n1, n2 := set.FindOverlapped(0)
	return n1 != nil, n1, n2
}

// IPMaskPrefixLen returns the number of prefix `1` for specfic IPMask.
func IPMaskPrefixLen(mask net.IPMask) int {
	maxBits := len(mask) * 8
	return sort.Search(maxBits, func(i int) bool {
		return i >= maxBits || (mask[i>>3]&(byte(1)<<(7-(i&0x7)))) == 0
	})
}

// IPMaskFromPrefixLen builds IPMask from prefix length.
func IPMaskFromPrefixLen(prefixLen uint, ipLen uint) (mask net.IPMask) {
	byteLen := prefixLen >> 3
	if byteLen >= ipLen {
		byteLen = ipLen
	}

	mask = make(net.IPMask, ipLen)
	for i := uint(0); i < byteLen; i++ {
		mask[i] = 0xFF
	}
	if ipLen <= byteLen {
		return
	}
	mask[byteLen] = byte(0xFF) << (8 - (prefixLen & 7))

	return
}

// IPNetSetEncode trys to marshal IPNetSet to bytes.
func IPNetSetEncode(s IPNetSet) ([]byte, error) {
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
		prefixLen := IPMaskPrefixLen(subnet.Mask)
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
func IPNetSetDecode(x []byte) (subnets IPNetSet, err error) {
	subnets = make(IPNetSet, 0)

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
			Mask: IPMaskFromPrefixLen(uint(prefixLen), uint(ipLen)),
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
