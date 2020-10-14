package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFestureSet(t *testing.T) {
	feats := new(FeatureSet)

	assert.False(t, feats.Enabled(30))
	feats.Enable(30)
	assert.True(t, feats.Enabled(30))
	t.Log(feats)

	feats.Enable(44)
	assert.True(t, feats.Enabled(44))
	t.Log(feats)

	feats.Disable(44)
	assert.False(t, feats.Enabled(44))
	t.Log(feats)

	b, err := feats.Encode()
	assert.NoError(t, err)

	newFeats := new(FeatureSet)
	assert.NoError(t, newFeats.Decode(b))
	assert.False(t, feats.Enabled(44))
	assert.True(t, feats.Enabled(30))

	assert.NoError(t, feats.Decode(b))
	assert.False(t, newFeats.Enabled(44))
	assert.True(t, newFeats.Enabled(30))

	feats2 := new(FeatureSet)
	assert.False(t, feats2.Enabled(44))
	assert.False(t, feats2.Enabled(2))
	feats.Enable(44)
	feats.Enable(2)
	assert.True(t, feats.Enabled(44))
	assert.True(t, feats.Enabled(2))

	feats3 := feats2.Clone()
	feats3.Merge(feats, false)
	assert.False(t, feats3.Enabled(2))
	assert.False(t, feats3.Enabled(30))
	assert.False(t, feats3.Enabled(44))

	feats2.Merge(feats, true)
	assert.True(t, feats2.Enabled(2))
	assert.True(t, feats2.Enabled(30))
	assert.True(t, feats2.Enabled(44))
}
