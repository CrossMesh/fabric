package metanet

import (
	"time"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/fabric/common"
	gossipUtils "github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/sladder"
)

const (
	// DefaultVersionInfoKey contains default key name for gossip.
	DefaultVersionInfoKey = "crossmesh_version"
)

func (n *MetadataNetwork) getFeatures() []int {
	feats := []int{}
	if n.Publish.Self.healthProbe {
		feats = append(feats, version.HealthProbing)
	}
	return feats
}

func (n *MetadataNetwork) initializeVersionInfo() error {
	versionModel := n.gossip.engine.WrapVersionKVValidator(&gossipUtils.VersionInfoValidatorV1{})
	modelKey := DefaultVersionInfoKey
	if err := n.gossip.cluster.RegisterKey(DefaultVersionInfoKey, versionModel, false, 0); err != nil {
		n.log.Errorf("failed to register version info model VersionInfoV1. [gossip key = \"%v\"] (err = \"%v\")", modelKey, err)
		return err
	}

	n.gossip.cluster.Keys(modelKey).Watch(n.onVersionInfo)

	// publish local version info.
	n.delayPublishVersionInfo(0)

	return nil
}

func (n *MetadataNetwork) updatePeerFeatures(node *MetaPeer, set version.FeatureSet) {
	node.healthProbe = set.Enabled(version.HealthProbing)
}

func (n *MetadataNetwork) updateForRawVersionInfo(node *sladder.Node, newValue string) {
	if node == nil {
		return
	}

	n.lock.RLock()
	peer, _ := n.peers[node]
	n.lock.RUnlock()
	if peer == nil {
		return
	}

	vi := gossipUtils.VersionInfoV1{}
	if err := vi.DecodeString(newValue); err != nil {
		n.log.Warn("cannot decode VersionInfoV1. (err = \"%v\")", err)
		return
	}

	n.updatePeerFeatures(peer, vi.Features)
}

func (n *MetadataNetwork) onVersionInfo(ctx *sladder.WatchEventContext, meta sladder.KeyValueEventMetadata) {
	switch meta.Event() {
	case sladder.KeyDelete:
		n.lock.RLock()
		peer, _ := n.peers[meta.Node()]
		n.lock.RUnlock()
		if peer == nil {
			return
		}
		n.updatePeerFeatures(peer, version.FeatureSet{})

	case sladder.KeyInsert:
		meta := meta.(sladder.KeyInsertEventMetadata)
		n.updateForRawVersionInfo(meta.Node(), meta.Value())

	case sladder.ValueChanged:
		meta := meta.(sladder.KeyChangeEventMetadata)
		n.updateForRawVersionInfo(meta.Node(), meta.New())
	}
}

func (n *MetadataNetwork) delayPublishVersionInfo(d time.Duration) {
	n.arbiters.main.Go(func() {
		if d > 0 {
			time.Sleep(d)
		}

		var errs common.Errors

		errs.Trace(n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(n.gossip.self, n.versionInfoKey)
			if err != nil {
				errs.Trace(err)
				return false
			}
			vi := rtx.(*gossipUtils.VersionInfoV1Txn)

			// build info.
			ver := version.GetBuildSemVer()
			if ver == nil {
				n.log.Warnf("got nil build version. build metadata may be incorrectly embeded. skip publishing. ")
			} else {
				vi.SetVersion(ver)
			}

			vi.AssignFeatures(n.getFeatures()...)

			return true
		}))

		if err := errs.AsError(); err != nil {
			n.log.Errorf("cannot publish local version info. (err = \"%v\")", err)
			n.delayPublishVersionInfo(time.Second * 5)
		}
	})
}
