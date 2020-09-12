package gossip

import (
	"testing"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/sladder"
	"github.com/stretchr/testify/assert"
)

func TestIdentity(t *testing.T) {
	resolver := NewPeerNameResolver()
	keys := resolver.Keys()
	assert.Equal(t, 1, len(keys))
	assert.Contains(t, keys, DefaultNetworkEndpointKey)

	set := NetworkEndpointSetV1{
		&NetworkEndpointV1{
			Type:     backend.TCPBackend,
			Priority: 10,
			Endpoint: "10.240.0.1:3880",
		},
		&NetworkEndpointV1{
			Type:     backend.TCPBackend,
			Priority: 10,
			Endpoint: "10.240.0.1:3891",
		},
		&NetworkEndpointV1{
			Type:     backend.TCPBackend,
			Priority: 9,
			Endpoint: "10.240.0.1:3898",
		},
	}
	set.Build()
	v1 := NetworkEndpointsV1{Version: 1, Endpoints: set.Clone()}
	s, err := v1.EncodeString()
	assert.NoError(t, err)

	names, err := resolver.Resolve(&sladder.KeyValue{
		Value: s, Key: resolver.NetworkEndpointKey,
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(names))
	assert.Contains(t, names, backend.TCPBackend.String()+":10.240.0.1:3880")
	assert.Contains(t, names, backend.TCPBackend.String()+":10.240.0.1:3891")
	assert.Contains(t, names, backend.TCPBackend.String()+":10.240.0.1:3898")
}
