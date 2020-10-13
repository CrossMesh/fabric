package metanet

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/sladder"
	logging "github.com/sirupsen/logrus"
)

// MetaPeerStateWatcher called before MetaPeer state changes.
// Return `false` to cancel subscription.
type MetaPeerStateWatcher func(peer *MetaPeer, new *MetaPeerStatePublication) bool

// MetaPeerStatePublication contains new peer state changes.
type MetaPeerStatePublication struct {
	Names     []string
	Endpoints []*MetaPeerEndpoint
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

type linkPathKey struct {
	local, remote string
	ty            backend.Type
}

func (p *linkPathKey) String() string {
	return "<" + backend.Endpoint{Type: p.ty, Endpoint: p.local}.String() +
		"," + backend.Endpoint{Type: p.ty, Endpoint: p.remote}.String() + ">"
}

type linkPath struct {
	linkPathKey

	backend.Backend
	Disabled bool
	cost     uint32
}

func (p *linkPath) String() string {
	s := p.linkPathKey.String() + ",cost=" + strconv.FormatUint(uint64(p.cost), 10)
	if p.Disabled {
		s += ",disabled"
	}
	return "<" + s + ">"
}

func (p *linkPath) Higher(x *linkPath) bool {
	if p == nil {
		return false
	}
	if x == nil {
		return true
	}
	if p.Disabled {
		return false
	}
	if x.Disabled {
		return true
	}
	return p.cost < x.cost
}

// MetaPeer contains basic information on metadata network.
type MetaPeer struct {
	*sladder.Node

	log *logging.Entry

	isSelf bool
	left   bool
	names  []string

	lock         sync.RWMutex
	customKeyMap map[string]interface{}
	intervalSubs map[*MetaPeerStateWatcher]struct{}

	Endpoints []*MetaPeerEndpoint // (COW. lock-free read)

	localEpoch     uint32
	linkPaths      []*linkPath // (COW)
	localEndpoints map[backend.Endpoint]backend.Backend
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

func (p *MetaPeer) filterLinkPath(filter func([]*linkPath) []*linkPath) {
	if filter == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	new := filter(p.linkPaths) // should make a deepcopy?
	if new == nil {
		return
	}

	sort.Slice(new, func(i, j int) bool { return new[i].Higher(new[j]) })
	p.linkPaths = new
}

func (p *MetaPeer) _rebuildLinkPath() {
	if p.isSelf {
		return
	}

	expectPathCount := len(p.localEndpoints) * len(p.Endpoints)
	newLinkPaths := make([]*linkPath, 0, expectPathCount)
	if expectPathCount < 1 {
		p.linkPaths = newLinkPaths
		return
	}

	oldSet := map[linkPathKey]*linkPath{}
	for _, path := range p.linkPaths {
		oldSet[path.linkPathKey] = path
	}

	localEndpoints := make([]backend.Endpoint, 0, len(p.localEndpoints))
	for local := range p.localEndpoints {
		localEndpoints = append(localEndpoints, local)
	}
	sort.Slice(localEndpoints, func(i, j int) bool { return localEndpoints[i].Type < localEndpoints[j].Type })

	for begin, end := 0, 0; begin < len(localEndpoints); begin = end {
		ty := localEndpoints[begin].Type
		end = sort.Search(len(localEndpoints)-begin, func(i int) bool {
			return localEndpoints[i+begin].Type > ty
		}) + begin

		for begin < end {
			local := localEndpoints[begin]
			backend := p.localEndpoints[local]
			for _, remote := range p.Endpoints {
				new := &linkPath{
					linkPathKey: linkPathKey{
						local: local.Endpoint, remote: remote.Endpoint.Endpoint,
						ty: backend.Type(),
					},
					Backend: backend,
				}
				if old, _ := oldSet[new.linkPathKey]; old != nil {
					new.Disabled = old.Disabled
				} else {
					new.Disabled = false
				}
				new.cost = (backend.Priority() + 1) * (remote.Priority + 1)
				newLinkPaths = append(newLinkPaths, new)
			}
			begin++
		}
	}
	sort.Slice(newLinkPaths, func(i, j int) bool { return newLinkPaths[i].Higher(newLinkPaths[j]) })

	p.linkPaths = newLinkPaths

	p.log.Debugf("rebuild link paths for %v: %v", p, newLinkPaths)
}

func (p *MetaPeer) publishInterval(prepare func(interval *MetaPeerStatePublication) bool) {
	if prepare == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	interval := &MetaPeerStatePublication{}
	if !prepare(interval) {
		return
	}

	for watch := range p.intervalSubs {
		if !(*watch)(p, interval) {
			delete(p.intervalSubs, watch)
		}
	}

	if interval.Endpoints != nil {
		p.Endpoints = interval.Endpoints
		p._rebuildLinkPath()
	}
	if interval.Names != nil {
		p.names = interval.Names
	}
}

func (p *MetaPeer) getLinkPaths(epoch uint32, localEndpoints map[backend.Endpoint]backend.Backend) []*linkPath {
	cacheEpoch, paths := p.localEpoch, p.linkPaths
	if epoch > cacheEpoch && localEndpoints != nil {
		p.lock.Lock()
		if cacheEpoch = p.localEpoch; epoch > cacheEpoch { // cache miss.
			p.localEndpoints = localEndpoints
			p._rebuildLinkPath()
			paths, p.localEpoch = p.linkPaths, epoch
		}
		p.lock.Unlock()
	}

	return paths
}

func (p *MetaPeer) chooseLinkPath(epoch uint32, localEndpoints map[backend.Endpoint]backend.Backend) *linkPath {
	paths := p.getLinkPaths(epoch, localEndpoints)

	if len(paths) < 1 {
		return nil
	}

	chosen := paths[0]
	if chosen.Disabled {
		chosen = nil
	}
	return chosen
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
