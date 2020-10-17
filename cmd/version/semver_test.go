package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSemVer(t *testing.T) {
	v1 := &SemVer{}
	v1.Parse("0.1.1-rc.1+revea3876dec")
	assert.Equal(t, uint8(0), v1.Major)
	assert.Equal(t, uint8(1), v1.Minor)
	assert.Equal(t, uint8(1), v1.Patch)
	if assert.Equal(t, 2, len(v1.Prerelease)) {
		assert.Equal(t, "rc", v1.Prerelease[0])
		assert.Equal(t, "1", v1.Prerelease[1])
	}
	if assert.Equal(t, 1, len(v1.Matadata)) {
		assert.Equal(t, "revea3876dec", v1.Matadata[0])
	}
	t.Log("v1 = ", v1)

	v1c := v1.Clone()
	assert.True(t, v1c.Equal(v1))
	assert.True(t, v1.Equal(v1c))
	assert.False(t, v1.Less(v1c))
	assert.False(t, v1c.Less(v1))

	v2 := &SemVer{}
	v2.Parse("0.1.1")
	t.Log("v2 = ", v2)
	assert.True(t, v1.Less(v2))
	assert.False(t, v2.Less(v1))
	assert.False(t, v1.Equal(v2))
	assert.False(t, v2.Equal(v1))

	v3 := &SemVer{}
	v3.Parse("0.1.2")
	t.Log("v3 = ", v3)
	assert.True(t, v2.Less(v3))
	assert.False(t, v3.Less(v2))
	assert.False(t, v3.Equal(v2))
	assert.False(t, v2.Equal(v3))

	v4 := &SemVer{}
	v4.Parse("0.2.1")
	t.Log("v4 = ", v4)
	assert.True(t, v3.Less(v4))
	assert.False(t, v4.Less(v3))
	assert.False(t, v4.Equal(v3))
	assert.False(t, v3.Equal(v4))

	v5 := &SemVer{}
	v5.Parse("1.1.1")
	t.Log("v5 = ", v5)
	assert.True(t, v4.Less(v5))
	assert.False(t, v5.Less(v4))
	assert.False(t, v5.Equal(v4))
	assert.False(t, v4.Equal(v5))

	v6 := &SemVer{}
	v6.Parse("0.1.1-2.dd")
	t.Log("v6 = ", v6)
	assert.True(t, v6.Less(v2))
	assert.False(t, v2.Less(v6))
	assert.False(t, v2.Equal(v6))
	assert.False(t, v6.Equal(v2))

	v7 := &SemVer{}
	v7.Parse("0.1.1-rc.1")
	t.Log("v7 = ", v7)
	assert.False(t, v7.Less(v6))
	assert.True(t, v6.Less(v7))
	assert.False(t, v6.Equal(v7))
	assert.False(t, v7.Equal(v6))

	v8 := &SemVer{}
	v8.Parse("0.1.1-rc.2")
	t.Log("v8 = ", v8)
	assert.True(t, v7.Less(v8))
	assert.False(t, v8.Less(v7))
	assert.False(t, v8.Equal(v7))
	assert.False(t, v7.Equal(v8))

	v9 := &SemVer{}
	v9.Parse("0.1.1-ra.2")
	t.Log("v9 = ", v9)
	assert.True(t, v9.Less(v8))
	assert.False(t, v8.Less(v9))
	assert.False(t, v8.Equal(v7))
	assert.False(t, v7.Equal(v8))

	v10 := &SemVer{}
	v10.Parse("0.1.1-rc.2.3")
	t.Log("v10 = ", v10)
	assert.True(t, v9.Less(v10))
	assert.False(t, v10.Less(v9))
	assert.False(t, v10.Equal(v9))
	assert.False(t, v9.Equal(v10))

	v11 := &SemVer{}
	v11.Parse("0.1.1-rc.2.3+rev121311.ddd.ccc")
	t.Log("v11 = ", v11)
	assert.False(t, v11.Less(v10))
	assert.False(t, v10.Less(v11))
	assert.True(t, v10.Equal(v11))
	assert.True(t, v11.Equal(v10))
	if assert.Equal(t, 3, len(v11.Prerelease)) {
		assert.Equal(t, "rc", v11.Prerelease[0])
		assert.Equal(t, "2", v11.Prerelease[1])
		assert.Equal(t, "3", v11.Prerelease[2])
	}
	if assert.Equal(t, 3, len(v11.Matadata)) {
		assert.Equal(t, "rev121311", v11.Matadata[0])
		assert.Equal(t, "ddd", v11.Matadata[1])
		assert.Equal(t, "ccc", v11.Matadata[2])
	}

	v12 := &SemVer{}
	v12.Parse("0.1.1-rc.2.3.new")
	t.Log("v12 = ", v12)
	assert.True(t, v10.Less(v12))
	assert.False(t, v12.Less(v10))
	assert.False(t, v10.Equal(v12))
	assert.False(t, v12.Equal(v10))
	if assert.Equal(t, 4, len(v12.Prerelease)) {
		assert.Equal(t, "rc", v12.Prerelease[0])
		assert.Equal(t, "2", v12.Prerelease[1])
		assert.Equal(t, "3", v12.Prerelease[2])
		assert.Equal(t, "new", v12.Prerelease[3])
	}
	assert.Equal(t, 0, len(v12.Matadata))
}
