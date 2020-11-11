package edgerouter

import (
	"net"

	"github.com/crossmesh/fabric/metanet"
)

type overlayEvent interface{}

// Basic Membership Events:

// PeerUnderlayEndpointInfo contains underlay endpoint information.
type PeerUnderlayEndpointInfo struct {
	PublicEndpoints   []net.IP
	InternalEndpoints []net.IP
}

// PeerBasicInfo contains basic information of peer in metadata network.
type PeerBasicInfo struct {
	Peer *metanet.MetaPeer
	PeerUnderlayEndpointInfo
	Region            string
	EdgeWeight        uint32
	UnderlayNetworkID uint32

	Tag []string // DO NOT USE NOW. NOT IMPLEMENTED SO FAR. This is a planning feature for traffic policy.
}

// PeerJoinEvent is fired when a peer joined network.
type PeerJoinEvent struct {
	PeerBasicInfo
}

// PeerLeaveEvent is fired when a peer left network.
type PeerLeaveEvent struct {
	Peer *metanet.MetaPeer
}

// UnderlayEndpointChangedEvent is fired when underlay network endpoints change.
type UnderlayEndpointChangedEvent struct {
	Peer     *metanet.MetaPeer
	Old, New *PeerUnderlayEndpointInfo
}

// PublicEndpointAdditions returns appended public endpoints.
func (e *UnderlayEndpointChangedEvent) PublicEndpointAdditions() []net.IP {
	return nil
}

// PublicEndpointDeletions returns appended public endpoints.
func (e *UnderlayEndpointChangedEvent) PublicEndpointDeletions() []net.IP {
	return nil
}

// PrivateEndpointAdditions returns appended public endpoints.
func (e *UnderlayEndpointChangedEvent) PrivateEndpointAdditions() []net.IP {
	return nil
}

// PrivateEndpointDeletions returns appended public endpoints.
func (e *UnderlayEndpointChangedEvent) PrivateEndpointDeletions() []net.IP {
	return nil
}

// EdgeWeightChangedEvent is fired when weight of edge peer changed.
type EdgeWeightChangedEvent struct {
	Peer     *metanet.MetaPeer
	Old, New uint32
}

// RegionChangedEvent is fired when region of peer changed.
type RegionChangedEvent struct {
	Old, New string
}

// UnderlayNetworkIDChangedEvent is fired when underlay network ID of peer changed.
type UnderlayNetworkIDChangedEvent struct {
	Old, New uint32
}

// Option Events:

// OptionSetEvent is fired when value of option is set.
type OptionSetEvent struct {
	Peer     *metanet.MetaPeer
	Name     string
	Old, New []byte
}

// OptionRemovedEvent is fired when an option was removed.
type OptionRemovedEvent struct {
	Peer *metanet.MetaPeer
	Name string
}
