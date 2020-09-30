package metanet

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/sladder"
)

// MetaPeerStateWatcher called before MetaPeer state changes.
// Return `false` to cancel subscription.
type MetaPeerStateWatcher func(peer *MetaPeer, new *MetaPeerStatePublication) bool

// MetaPeerStatePublication contains new peer state changes.
type MetaPeerStatePublication struct {
	Names     []string
	Endpoints []MetaPeerEndpoint
}

// String formats a human-friendly string of a state publication.
func (i *MetaPeerStatePublication) String() string {
	changes := []string(nil)

	if i.Endpoints != nil {
		changes = append(changes, fmt.Sprintf("endpoints(%v)", i.Endpoints))
	}
	if i.Names != nil {
		changes = append(changes, fmt.Sprintf("names(%v)", i.Names))
	}
	return "<" + strings.Join(changes, ",") + ">"
}

// Trival reports whether publication has no change.
func (i *MetaPeerStatePublication) Trival() bool {
	return i.Names == nil && i.Endpoints == nil
}

// MetaPeer contains basic information on metadata network.
type MetaPeer struct {
	*sladder.Node
	isSelf bool
	left   bool
	names  []string

	lock         sync.RWMutex
	customKeyMap map[string]interface{}

	Endpoints    []MetaPeerEndpoint // (COW. safe to lock-free read and lock-free write except slice itself)
	intervalSubs map[*MetaPeerStateWatcher]struct{}
}

func (p *MetaPeer) String() (s string) {
	if p.Node != nil {
		s = p.PrintableName()
	}
	if p.isSelf {
		s = "self," + s
	}
	return "meta_peer<" + s + ">"
}

// SladderNode returns related *sladder.Node.
func (p *MetaPeer) SladderNode() *sladder.Node { return p.Node }

// HashID returns unique id for hash.
// This ID will nerver change within lifecycle of peer.
func (p *MetaPeer) HashID() string {
	ptr := uintptr(unsafe.Pointer(p))

	switch sz := unsafe.Sizeof(ptr); sz {
	case 8:
		var bin [8]byte
		binary.LittleEndian.PutUint64(bin[:], uint64(ptr))
		return string(bin[:])

	case 4:
		var bin [4]byte
		binary.LittleEndian.PutUint32(bin[:], uint32(ptr))
		return string(bin[:])

	default:
		panic(fmt.Sprintf("unsupported machine architecture with %v-bit pointer", sz))
	}
}

// IsSelf reports the peer is myself.
func (p *MetaPeer) IsSelf() bool { return p.isSelf }

// Names reports node names.
func (p *MetaPeer) Names() (names []string) {
	names = append(names, p.names...)
	return
}

func (p *MetaPeer) publishInterval(interval *MetaPeerStatePublication) {
	p.lock.Lock()
	defer p.lock.Unlock()

	for watch := range p.intervalSubs {
		if !(*watch)(p, interval) {
			delete(p.intervalSubs, watch)
		}
	}
	if interval.Endpoints != nil {
		p.Endpoints = interval.Endpoints
	}
	if interval.Names != nil {
		p.names = interval.Names
	}
}

func (p *MetaPeer) chooseEndpoint() backend.Endpoint {
	endpoints := p.Endpoints
	if len(endpoints) < 1 {
		return backend.NullEndpoint
	}
	selected := endpoints[0]
	if selected.Disabled {
		return backend.NullEndpoint
	}
	return selected.Endpoint
}

// SubscribeEndpointsInterval register callback for Endpoints changes.
func (p *MetaPeer) SubscribeEndpointsInterval(watch MetaPeerStateWatcher) {
	if watch == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()
	if p.intervalSubs == nil {
		p.intervalSubs = map[*MetaPeerStateWatcher]struct{}{}
	}
	p.intervalSubs[&watch] = struct{}{}
}

// Set sets custum local key value.
func (p *MetaPeer) Set(k string, v interface{}) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.customKeyMap[k] = v
}

// Get gets custum local key value.
func (p *MetaPeer) Get(k string) interface{} {
	p.lock.RLock()
	defer p.lock.RUnlock()

	v, _ := p.customKeyMap[k]
	return v
}
