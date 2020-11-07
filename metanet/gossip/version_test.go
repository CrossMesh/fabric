package gossip

import (
	"testing"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/sladder"
	"github.com/stretchr/testify/assert"
)

func TestVersionInfo(t *testing.T) {
	v11 := &VersionInfoV1{MetaVersion: 0}
	assert.Error(t, v11.Validate())
	v11.MetaVersion = 1
	assert.NoError(t, v11.Validate())

	assert.True(t, v11.Version.Parse("0.1.2-rc.1+rev2829"))
	v11.Features.Enable(1)
	v11.Features.Enable(40)
	v11.Features.Enable(27)

	v11c := v11.Clone()
	assert.True(t, v11.Equal(v11c))
	assert.True(t, v11c.Equal(v11))

	v11s, err := v11.EncodeToString()
	assert.NoError(t, err)
	v11d := &VersionInfoV1{}
	assert.NoError(t, v11d.DecodeString(v11s))

	v12 := &VersionInfoV1{MetaVersion: 1}
	assert.True(t, v12.Version.Parse("0.1.3-rc.2+rev2829.327"))
	v12.Features.Enable(40)
	v12.Features.Enable(20)
	var v12s string
	v12s, err = v12.EncodeToString()
	assert.NoError(t, err)

	getTx1 := func() *VersionInfoV1Txn {
		tx1, err := createTxn(&sladder.KeyValue{Value: v11s})
		assert.Equal(t, v11s, tx1.Before())
		assert.NoError(t, err)
		assert.NotNil(t, tx1)
		assert.Equal(t, v11s, tx1.Before())
		assert.True(t, tx1.Version().Equal(&v11.Version))
		return tx1
	}
	getTx2 := func() *VersionInfoV1Txn {
		tx2, err := createTxn(&sladder.KeyValue{Value: v12s})
		assert.Equal(t, v12s, tx2.Before())
		assert.NoError(t, err)
		assert.NotNil(t, tx2)
		assert.Equal(t, v12s, tx2.Before())
		assert.True(t, tx2.Version().Equal(&v12.Version))
		return tx2
	}

	vdr := &VersionInfoValidatorV1{}
	{
		rtx, err := vdr.Txn(sladder.KeyValue{Value: v11s})
		assert.NoError(t, err)
		assert.NotNil(t, rtx)
		assert.True(t, vdr.Validate(sladder.KeyValue{Value: v11s}))
		assert.True(t, vdr.Validate(sladder.KeyValue{Value: v12s}))
	}
	// merging.
	{
		tx1, tx2 := getTx1(), getTx2()
		// non-concurrent merge.
		t.Log(tx1.new, tx1.old)
		assert.False(t, tx1.Updated())
		tx1.Merge(tx2, false)
		assert.True(t, tx1.Updated())
		after := &VersionInfoV1{}
		after.DecodeString(tx1.After())
		assert.True(t, after.Equal(v12))
		// concurrent merge.
		tx1, tx2 = getTx1(), getTx2()
		t.Log(tx1.new, tx1.old)
		assert.False(t, tx1.Updated())
		tx1.Merge(tx2, true)
		assert.True(t, tx1.Updated())
		after = &VersionInfoV1{}
		after.DecodeString(tx1.After())
		assert.False(t, after.Equal(v12))

		tx1, tx2 = getTx1(), getTx2()
		newVer := &version.SemVer{}
		newVer.Parse("0.1.2-rc.1+rev2839")
		tx2.SetVersion(newVer)
		assert.True(t, tx2.Updated())
		assert.False(t, tx1.Updated())
		tx1.Merge(tx2, true)
		assert.True(t, tx1.Updated())
		after = &VersionInfoV1{}
		after.DecodeString(tx1.After())
		assert.False(t, after.Equal(v12))
		assert.True(t, after.Version.Equal(newVer))

		tx1, tx2 = getTx1(), getTx2()
		newVer = &version.SemVer{}
		newVer.Parse("0.1.2-rc.1+rev2829.1")
		tx2.SetVersion(newVer)
		assert.True(t, tx2.Updated())
		assert.False(t, tx1.Updated())
		tx1.Merge(tx2, true)
		assert.True(t, tx1.Updated())
		after = &VersionInfoV1{}
		after.DecodeString(tx1.After())
		assert.False(t, after.Equal(v12))
		assert.True(t, after.Version.Equal(newVer))
	}
	// ops.
	{
		tx := getTx1()
		assert.False(t, tx.Updated())
		tx.Enable(4)
		tx.Enable(5)
		tx.Enable(6)
		assert.True(t, tx.Enabled(4))
		assert.True(t, tx.Enabled(5))
		assert.True(t, tx.Enabled(6))
		assert.True(t, tx.Updated())
		tx.Disable(4)
		tx.Disable(5)
		tx.Disable(6)
		assert.False(t, tx.Updated())
		tx.AssignFeatures(4, 5, 6)
		assert.True(t, tx.Updated())
		after := &VersionInfoV1{}
		after.DecodeString(tx.After())
		assert.True(t, after.Features.Enabled(4))
		assert.True(t, after.Features.Enabled(5))
		assert.True(t, after.Features.Enabled(6))
	}
	// sync
	{
		local := &sladder.KeyValue{Value: v12s}
		changed, err := vdr.Sync(nil, &sladder.KeyValue{Value: v11s})
		assert.NoError(t, err)
		assert.False(t, changed)

		local.Value = v12s
		changed, err = vdr.Sync(local, nil)
		assert.NoError(t, err)
		assert.True(t, changed)

		local.Value = v12s
		changed, err = vdr.Sync(nil, nil)
		assert.NoError(t, err)
		assert.False(t, changed)

		local.Value = v12s
		changed, err = vdr.Sync(local, &sladder.KeyValue{Value: v11s})
		assert.NoError(t, err)
		assert.True(t, changed)
		after := &VersionInfoV1{}
		assert.NoError(t, after.DecodeString(local.Value))
		assert.True(t, v11.Equal(after))

		local.Value = v12s
		changed, err = vdr.SyncEx(local, &sladder.KeyValue{Value: v11s}, &MockMergingProps{concurrent: true})
		assert.NoError(t, err)
		assert.True(t, changed)
		after = &VersionInfoV1{}
		assert.NoError(t, after.DecodeString(local.Value))
		assert.False(t, v11.Equal(after))
	}
}
