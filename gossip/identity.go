package gossip

import (
	"sort"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/sladder"
)

// PeerNameResolver resolves peer name from metadata.
type PeerNameResolver struct {
	NetworkEndpointKey string
}

// NewPeerNameResolver creates new PeerNameResolver with default parameters.
func NewPeerNameResolver() *PeerNameResolver {
	return &PeerNameResolver{
		NetworkEndpointKey: DefaultNetworkEndpointKey,
	}
}

// BuildNodeName builds node name from endpoint.
func BuildNodeName(ep backend.Endpoint) string { return ep.Type.String() + ":" + ep.Endpoint }

func (r *PeerNameResolver) fromNetworkEndpointV1(v1 *NetworkEndpointsV1) (names []string) {
	for _, ep := range v1.Endpoints {
		if ep.Type == backend.UnknownBackend || ep.Endpoint == "" {
			continue
		}
		names = append(names, BuildNodeName(backend.Endpoint{
			Type: ep.Type, Endpoint: ep.Endpoint,
		}))
	}
	return
}

// Keys returns key list for watching.
func (r *PeerNameResolver) Keys() []string {
	return []string{r.NetworkEndpointKey}
}

// Resolve resolves peer names.
func (r *PeerNameResolver) Resolve(kvs ...*sladder.KeyValue) (names []string, err error) {
	var src *sladder.KeyValue

	for _, kv := range kvs {
		if kv.Key == r.NetworkEndpointKey {
			src = kv
		}
	}
	if src == nil {
		return nil, nil
	}

	// V1 only so far.
	v1 := NetworkEndpointsV1{}
	if err = v1.DecodeStringAndValidate(src.Value); err != nil {
		return
	}
	names = r.fromNetworkEndpointV1(&v1)

	if len(names) > 1 { // deduplicate names.
		sort.Strings(names)
		rear := 1
		for last, head := names[0], 1; head < len(names); head++ {
			if last == names[head] {
				continue
			}
			names[rear] = names[head]
			rear++
		}
		names = names[:rear]
	}

	return names, nil
}
