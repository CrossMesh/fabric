package version

import (
	"bytes"
	"errors"
)

const (
	// HealthProbing is ID of metanet's health probing feature.
	HealthProbing = 0
)

var (
	// ErrTooManyFeatures raised when features exceeds capacity of FeatureSet
	ErrTooManyFeatures = errors.New("too many features")

	// ErrBrokenSet raised when FeatureSet is broken.
	ErrBrokenSet = errors.New("broken feature set")
)

// FeatureNames maps feature to it's name.
var FeatureNames map[int]string = map[int]string{
	HealthProbing: "health_probe",
}

// FeatureSet contains feature enabling states.
type FeatureSet []byte

// Clone makes a deep copy.
func (s FeatureSet) Clone() (new FeatureSet) {
	new = append(new, s...)
	return
}

// Equal reports whether two FeatureSet are equal.
func (s *FeatureSet) Equal(r *FeatureSet) bool {
	if s == r {
		return true
	}
	if s == nil || r == nil {
		return false
	}
	return bytes.Compare(*s, *r) == 0
}

// Encode marshals contents.
func (s *FeatureSet) Encode() (b []byte, err error) {
	if len(*s) < 1 {
		return []byte{}, nil
	}
	length := len(*s)
	if length > 0xFF {
		return nil, errors.New("too many features")
	}
	b = append(b, byte(length))
	b = append(b, *s...)
	return b, nil
}

// Decode unmarshals contents.
func (s *FeatureSet) Decode(b []byte) error {
	if len(b) < 1 {
		return nil
	}
	length := b[0]
	if int(length) > len(b)-1 {
		return ErrBrokenSet
	}
	if *s != nil {
		*s = (*s)[:0]
	}
	b = b[1:]
	*s = append(*s, b[:length]...)
	return nil
}

// Merge merges two FeatureSet.
func (s *FeatureSet) Merge(r *FeatureSet, enableFirst bool) (changed bool) {
	changed = false

	if len(*s) < len(*r) {
		new := make(FeatureSet, len(*r))
		copy(new[:len(*s)], *s)
		*s = new
	}
	end := len(*s)
	if len(*r) < end {
		end = len(*r)
	}
	if enableFirst {
		for idx := 0; idx < end; idx++ {
			ro, r := &(*s)[idx], (*r)[idx]
			if *ro != r {
				changed = true
			}
			*ro |= r
		}
	} else {
		for idx := 0; idx < end; idx++ {
			ro, r := &(*s)[idx], (*r)[idx]
			if *ro != r {
				changed = true
			}
			*ro &= r
		}
		*s = (*s)[:end]
	}

	return
}

func (s *FeatureSet) getTestBit(feature int, extend bool) (*byte, byte) {
	if feature < 0 {
		return nil, 0
	}
	idx := feature >> 3
	if (idx + 1) > len(*s) {
		if extend {
			new := make([]byte, idx+1)
			copy(new[:len(*s)], *s)
			*s = new
		} else {
			return nil, 0
		}
	}
	bit := byte(1) << (7 - (feature & 0x7))
	return &(*s)[idx], bit
}

// Enabled reports whether feature with specific ID is enabled.
func (s *FeatureSet) Enabled(feature int) bool {
	ref, bit := s.getTestBit(feature, false)
	if ref == nil {
		return false
	}
	return *ref&bit != 0
}

// Enable enables feature.
func (s *FeatureSet) Enable(feature int) (origin bool) {
	ref, bit := s.getTestBit(feature, true)
	origin = *ref&bit != 0
	*ref |= bit
	return
}

// Disable disables feature.
func (s *FeatureSet) Disable(feature int) (origin bool) {
	ref, bit := s.getTestBit(feature, true)
	origin = *ref&bit != 0
	*ref &= ^bit
	return
}
