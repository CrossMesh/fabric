package metanet

import (
	"time"

	"github.com/crossmesh/fabric/cmd/version"
	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/gossip"
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

	n.versionInfoKey = modelKey

	n.gossip.cluster.Keys(modelKey).Watch(n.onVersionInfo)
	n.gossip.cluster.Watch(n.versionInfoOnMembership)

	// publish local version info.
	n.delayPublishVersionInfo(0)

	return nil
}

func (n *MetadataNetwork) delayProcessVersionInfoForPeer(peer *MetaPeer, d time.Duration) {
	n.arbiters.main.Go(func() {
		if d > 0 {
			time.Sleep(d)
		}

		var errs common.Errors

		n.gossip.cluster.Txn(func(t *sladder.Transaction) bool {
			rtx, err := t.KV(n.gossip.self, n.versionInfoKey)
			if err != nil {
				errs.Trace(err)
				return false
			}
			vi := rtx.(*gossipUtils.VersionInfoV1Txn)

			n.updateFromVersionInfo(peer, vi.VersionInfoV1())

			return false
		})
	})
}

func (n *MetadataNetwork) versionInfoOnMembership(ctx *sladder.ClusterEventContext, event sladder.Event, node *sladder.Node) {
	switch event {
	case sladder.NodeJoined:
		n.lock.RLock()
		peer, _ := n.peers[node]
		n.lock.RUnlock()
		if peer == nil {
			return
		}
		n.delayProcessVersionInfoForPeer(peer, 0)
	}
}

func (n *MetadataNetwork) updatePeerFeatures(node *MetaPeer, set version.FeatureSet) {
	node.healthProbe = set.Enabled(version.HealthProbing)
}

func (n *MetadataNetwork) updateFromVersionInfo(peer *MetaPeer, vi *gossip.VersionInfoV1) {
	n.updatePeerFeatures(peer, vi.Features)
}

func (n *MetadataNetwork) updateFromRawVersionInfo(node *sladder.Node, newValue string) {
	if node == nil {
		return
	}
	if node == n.gossip.self {
		n.enforceSelfVersionInfoRaw(newValue)
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

	n.updateFromVersionInfo(peer, &vi)
}

func (n *MetadataNetwork) enforceSelfVersionInfo(v1 *gossipUtils.VersionInfoV1) {
	if v1 == nil {
		n.delayPublishVersionInfo(0)
		return
	}

	ver := version.BuildSemVer
	if ver == nil {
		n.log.Warnf("got nil build version. build metadata may be incorrectly embeded. skip checking. ")
	} else if !v1.Version.Equal(ver) {
		n.delayPublishVersionInfo(0)
		return
	}

	features := version.FeatureSet{}
	for _, feat := range n.getFeatures() {
		features.Enable(feat)
	}
	if !features.Equal(&v1.Features) {
		n.delayPublishVersionInfo(0)
		return
	}

}

func (n *MetadataNetwork) enforceSelfVersionInfoRaw(raw string) {
	v1 := &gossipUtils.VersionInfoV1{}
	err := v1.DecodeString(raw)
	if err != nil {
		n.delayPublishVersionInfo(0)
		n.log.Warn("enforceSelfVersionInfoRaw() got invalid version info stream. republishing version info...")
		return
	}
	n.enforceSelfVersionInfo(v1)
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
		if peer == n.self {
			n.enforceSelfVersionInfo(nil)
		} else {
			n.updatePeerFeatures(peer, version.FeatureSet{})
		}

	case sladder.KeyInsert:
		meta := meta.(sladder.KeyInsertEventMetadata)
		n.updateFromRawVersionInfo(meta.Node(), meta.Value())

	case sladder.ValueChanged:
		meta := meta.(sladder.KeyChangeEventMetadata)
		n.updateFromRawVersionInfo(meta.Node(), meta.New())
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
			ver := version.BuildSemVer
			if ver == nil {
				n.log.Warnf("got nil build version. build metadata may be incorrectly embeded. skip publishing. ")
			} else {
				vi.SetVersion(ver)
			}

			vi.AssignFeatures(n.getFeatures()...)

			return vi.Updated()
		}))

		if err := errs.AsError(); err != nil {
			n.log.Errorf("cannot publish local version info. (err = \"%v\")", err)
			n.delayPublishVersionInfo(time.Second * 5)
		}
	})
}
