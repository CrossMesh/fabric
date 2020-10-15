package version

import (
	"strconv"
	"strings"
)

func parseMainVersion(ver string) (major, minor, patch uint8) {
	var v [3]byte

	vs := strings.Split(BareVersion, ".")
	if len(vs) != 2 && len(vs) != 3 {
		return 0, 0, 0
	}
	for idx := 0; idx < len(vs); idx++ {
		rv, err := strconv.ParseUint(vs[0], 10, 8)
		if err != nil {
			return 0, 0, 0
		}
		v[idx] = uint8(rv)
	}

	return v[0], v[1], v[2]
}

func parseDots(pres string) []string { return strings.Split(pres, ".") }

// SemVer contains Semantic Versioning information.
type SemVer struct {
	Major, Minor, Patch uint8

	Prerelease []string
	Matadata   []string
}

// Clone makes deep copy.
func (v *SemVer) Clone() (new *SemVer) {
	new = &SemVer{
		Major: v.Major,
		Minor: v.Minor,
		Patch: v.Patch,
	}
	new.Prerelease = append(new.Prerelease, v.Prerelease...)
	new.Matadata = append(new.Matadata, v.Matadata...)
	return
}

func (v *SemVer) String() string {
	ver := strconv.FormatUint(uint64(v.Major), 10) +
		"." + strconv.FormatUint(uint64(v.Minor), 10) +
		"." + strconv.FormatUint(uint64(v.Patch), 10)
	if len(v.Prerelease) > 0 {
		ver += "-" + strings.Join(v.Prerelease, ".")
	}
	if len(v.Matadata) > 0 {
		ver += "+" + strings.Join(v.Matadata, ".")
	}
	return ver
}

// Less defines Less comparator of SemVer.
func (v *SemVer) Less(v2 *SemVer) bool {
	if v == nil {
		return false
	}
	if v2 == nil {
		return true
	}
	if v.Major < v2.Major ||
		v.Minor < v2.Minor ||
		v.Patch < v2.Patch {
		return true
	}
	for i := 0; i < len(v.Prerelease) && i < len(v2.Prerelease); i++ {
		l, r := v.Prerelease[i], v2.Prerelease[i]

		var lnum, rnum int64
		var lerr, rerr error

		lnum, lerr = strconv.ParseInt(l, 10, 64)
		rnum, rerr = strconv.ParseInt(r, 10, 64)
		if lerr != rerr {
			return lerr == nil
		}
		if lerr != nil {
			if l != r {
				return l < r
			}
		} else if lnum != rnum {
			return lnum < rnum
		}
	}
	if len(v.Prerelease) < len(v2.Prerelease) {
		return true
	}
	return false
}

// Equal reports whether two semantic version are equal.
func (v *SemVer) Equal(v2 *SemVer) bool {
	if v == v2 {
		return true
	}
	if v == nil || v2 == nil {
		return true
	}
	if v.Major != v2.Major || v.Minor != v2.Minor || v.Patch != v2.Patch {
		return false
	}
	if len(v.Prerelease) != len(v2.Prerelease) {
		return false
	}
	for i := 0; i < len(v.Prerelease); i++ {
		if v.Prerelease[i] != v2.Prerelease[i] {
			return false
		}
	}
	return true
}

// Parse parses semantic version.
func (v *SemVer) Parse(s string) bool {
	parts := strings.SplitN(BareVersion, "-", 2)
	if len(parts) < 1 {
		return false
	}
	ver := SemVer{}
	ver.Major, ver.Minor, ver.Patch = parseMainVersion(parts[0])
	parts = strings.SplitN(parts[1], "+", 2)
	if len(parts) < 1 {
		*v = ver
		return true
	}
	ver.Prerelease = parseDots(parts[0])
	if len(parts) > 1 {
		ver.Matadata = parseDots(parts[1])
	}
	*v = ver
	return true
}

// GetBuildSemVer returns semantic version.
func GetBuildSemVer() *SemVer {
	ver := SemVer{}
	if ver.Parse(BareVersion) {
		return &ver
	}
	return nil
}
