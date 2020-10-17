package metanet

import (
	"container/heap"
	"encoding/binary"
	"strconv"
	"time"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/proto"
)

type endpointProbingContext struct {
	path linkPathKey

	id           uint64
	tryCount     int
	since, tryAt time.Time
	peer         *MetaPeer
}

type lastProbingContext struct {
	path linkPathKey
	id   uint64
	peer *MetaPeer
	at   time.Time
}

const defaultHealthyCheckProbeBrust = 5
const defaultHealthyCheckProbeTimeout = time.Second * 10

// ErrMessageStreamTooShort raised when there are no enough bytes to decode message.
// Mostly, it indicates that the message stream is incompleted.
var ErrMessageStreamTooShort = &MessageBrokenError{reason: "stream too short"}

func lastestCandidateOlder(a, b *lastProbingContext) bool {
	if b.id == 0 {
		return false
	}
	if a.id == 0 {
		return true
	}
	return a.at.Before(b.at)
}

type latestProbingCandidatesQueue []*lastProbingContext

func (q latestProbingCandidatesQueue) Len() int            { return len(q) }
func (q latestProbingCandidatesQueue) Less(i, j int) bool  { return !lastestCandidateOlder(q[i], q[j]) }
func (q latestProbingCandidatesQueue) Swap(i, j int)       { q[i], q[j] = q[j], q[i] }
func (q *latestProbingCandidatesQueue) Push(x interface{}) { *q = append(*q, x.(*lastProbingContext)) }
func (q *latestProbingCandidatesQueue) Pop() (x interface{}) {
	i := len(*q) - 1
	x = (*q)[i]
	*q = (*q)[:i]
	return
}

type endpointProbeHeader struct {
	id         uint64
	isResponse bool
}

func (r *endpointProbeHeader) Encode() (bins []byte) {
	var buf [8]byte

	bins = append(bins, 0x01) // version.
	if r.isResponse {
		bins = append(bins, 0x01)
	} else {
		bins = append(bins, 0x00)
	}
	binary.BigEndian.PutUint64(buf[:], r.id)
	bins = append(bins, buf[:]...)

	return
}

func (r *endpointProbeHeader) Decode(bins []byte) (int, error) {
	if len(bins) < 1 {
		return 0, ErrMessageStreamTooShort
	}

	version := bins[0]
	if version != 0x01 {
		return 0, &MessageBrokenError{
			reason: "version unmatched (expect " + strconv.FormatUint(1, 10) + ", but got " + strconv.FormatInt(int64(version), 10) + ")",
		}
	}
	bins = bins[1:]

	if len(bins) < 9 {
		return 0, ErrMessageStreamTooShort
	}
	typeMagic := bins[0]
	r.isResponse = typeMagic == 1
	r.id = binary.BigEndian.Uint64(bins[1:9])

	return 10, nil
}

type endpointProbeRequest struct {
	endpointProbeHeader
}

type endpointProbeResponse struct {
	endpointProbeHeader
}

func isUnhealthyPath(ctx *endpointProbingContext) bool {
	return ctx != nil && ctx.tryCount > 2
}

func (n *MetadataNetwork) initializeEndpointHealthCheck() {
	n.RegisterMessageHandler(proto.MsgTypePing, n.onHealthProbingMessage)
	n.goHealthProbe()
}

func (n *MetadataNetwork) onHealthProbingRequest(header *endpointProbeHeader, msg *Message, bodyPayload []byte) {
	resp := endpointProbeResponse{
		endpointProbeHeader: *header,
	}
	resp.isResponse = true
	payload := resp.Encode()
	n.log.Debugf("sending probe response to %v. [via = %v, request ID = %v]", msg.Endpoint, msg.Via, header.id)
	n.SendViaEndpoint(proto.MsgTypePing, payload, msg.Via, msg.Endpoint.Endpoint)
}

func (n *MetadataNetwork) onHealthProbingResponse(header *endpointProbeHeader, msg *Message, bodyPayload []byte) {
	n.probeLock.Lock()
	defer n.probeLock.Unlock()

	path := linkPathKey{
		ty:     msg.Endpoint.Type,
		remote: msg.Endpoint.Endpoint,
		local:  msg.Via.Endpoint,
	}
	ctx, _ := n.probes[path]
	if ctx == nil || ctx.id != header.id || ctx.path != path {
		return
	}
	delete(n.probes, path)

	n.recentSuccesses[path] = &lastProbingContext{
		path: path,
		id:   header.id,
		peer: ctx.peer,
		at:   time.Now(),
	}

	last := isUnhealthyPath(ctx)
	ctx.tryCount = 0

	if next := isUnhealthyPath(ctx); next != last {
		ctx.peer.filterLinkPath(func(old []*linkPath) []*linkPath {

			for _, currentPath := range old {
				if currentPath.linkPathKey != path {
					continue
				}
				if currentPath.Disabled != next {
					if next {
						n.log.Warnf("disabled unhealthy path %v.", &path)
					} else {
						n.log.Infof("enabled healthy path %v.", path)
					}
					currentPath.Disabled = next
					return old
				}
			}

			return nil
		})
	}
}

func (n *MetadataNetwork) onHealthProbingMessage(msg *Message) {
	header := endpointProbeHeader{}
	used, err := header.Decode(msg.Payload)
	if err != nil {
		n.log.Error("failed to decode a endpoint probing message. [from = %v, via = %v] (err = \"%v\")", msg.Endpoint, msg.Via, err)
		return
	}
	if header.isResponse {
		n.onHealthProbingResponse(&header, msg, msg.Payload[used:])
	} else {
		n.onHealthProbingRequest(&header, msg, msg.Payload[used:])
	}
}

func (n *MetadataNetwork) _searchForProbeTargets(need int, newProbeCandidates []*endpointProbingContext) (targets, delayed []*endpointProbingContext) {
	now := time.Now()

	type pathPeer struct {
		path linkPathKey
		peer *MetaPeer
	}

	matchSet := map[pathPeer]*endpointProbingContext{}

	probesMatchers := []func(*endpointProbingContext) bool{
		func(ctx *endpointProbingContext) bool { return ctx.tryCount == 0 },
		func(ctx *endpointProbingContext) bool {
			return now.Sub(ctx.tryAt) >= n.ProbeTimeout
		},
		func(ctx *endpointProbingContext) bool { return isUnhealthyPath(ctx) },
	}

	probesMatch := func(matcherIdx int) func() {
		return func() {
		matchNextProbe:
			for _, ctx := range n.probes {
				if need <= len(matchSet) {
					break matchNextProbe
				}

				if !probesMatchers[matcherIdx](ctx) {
					continue
				}

				matchSet[pathPeer{
					path: ctx.path,
					peer: ctx.peer,
				}] = ctx
			}
		}
	}

	targetSearchers := []func(){
		probesMatch(0), // delayed new probing candidates.
		probesMatch(1), // timeout probes.
		// oldestly-probed endpoint.
		func() {
			if need <= len(targets) {
				return
			}

			numOfRequired := need - len(targets)
			latestCandidates := latestProbingCandidatesQueue(nil)

			for _, peer := range n.peers {
				if peer.IsSelf() || !peer.healthProbe {
					continue
				}

				paths := peer.getLinkPaths(n.Publish.Epoch, n.Publish.Backends)
				for _, path := range paths {
					latestProbingCtx, _ := n.recentSuccesses[path.linkPathKey]
					if latestProbingCtx != nil {
						if latestProbingCtx.peer != peer {
							latestProbingCtx.peer = peer
							latestProbingCtx.id = 0
							delete(n.recentSuccesses, path.linkPathKey)
						}
					} else if ctx, _ := n.probes[path.linkPathKey]; ctx != nil {
						latestProbingCtx = &lastProbingContext{
							path: path.linkPathKey,
							id:   ctx.id,
							peer: peer,
							at:   ctx.tryAt,
						}
					} else {
						latestProbingCtx = &lastProbingContext{
							path: path.linkPathKey,
							id:   0,
							peer: peer,
						}
					}

					numOfLastest := len(latestCandidates)
					if numOfLastest < numOfRequired {
						latestCandidates = append(latestCandidates, latestProbingCtx)
						if numOfLastest+1 == numOfRequired {
							heap.Init(&latestCandidates)
						}
					} else if lastestCandidateOlder(latestProbingCtx, latestCandidates[0]) {
						heap.Pop(&latestCandidates)
						heap.Push(&latestCandidates, latestProbingCtx)
					}
				}
			}

			for _, latestProbingCtx := range latestCandidates {
				ctx, _ := n.probes[latestProbingCtx.path]
				if ctx == nil {
					ctx = &endpointProbingContext{
						path:     latestProbingCtx.path,
						id:       0,
						tryCount: 0,
						since:    now,
						tryAt:    now,
						peer:     latestProbingCtx.peer,
					}
				}
				matchSet[pathPeer{
					path: latestProbingCtx.path,
					peer: latestProbingCtx.peer,
				}] = ctx
			}
		},
		probesMatch(2), // unhealthy endpoints.
	}

	// new probing candidates first.
	if need := need - len(targets); need <= len(newProbeCandidates) {
		targets = append(targets, newProbeCandidates[:need]...)
		delayed = newProbeCandidates[need:]
		return
	}
	targets = append(targets, newProbeCandidates...)
	need -= len(newProbeCandidates)

	// search for more targets by rule set.
	for i := 0; i < len(targetSearchers) && need > len(targets); i++ {
		search := targetSearchers[i]
		search()
	}

	for _, ctx := range matchSet {
		targets = append(targets, ctx)
	}

	return
}

func (n *MetadataNetwork) goHealthProbe() {
	n.arbiters.main.TickGo(func(cancel func(), deadline time.Time) {
		n.lock.RLock()
		defer n.lock.RUnlock()

		n.probeLock.Lock()
		defer n.probeLock.Unlock()

		now := time.Now()

		newProbeCandidates := []*endpointProbingContext{}
		disablingLog := map[*MetaPeer]map[linkPathKey]bool{}

		setDisable := func(peer *MetaPeer, path linkPathKey, disabled bool) {
			paths, _ := disablingLog[peer]
			if paths == nil {
				paths = make(map[linkPathKey]bool)
				disablingLog[peer] = paths
			}
			paths[path] = disabled
		}

		// deal with recent failures.
		n.lastFails.Range(func(k, v interface{}) bool {
			path, isEndpoint := k.(linkPathKey)
			if !isEndpoint {
				n.lastFails.Delete(k)
				return true
			}
			peer, isPeer := v.(*MetaPeer)
			if !isPeer || peer == nil || peer.IsSelf() {
				n.lastFails.Delete(k)
				return true
			}

			if !peer.healthProbe { // not ready to be probed.
				n.lastFails.Delete(k)
				return true
			}

			ctx, _ := n.probes[path]
			if ctx == nil {
				ctx = &endpointProbingContext{
					path:     path,
					tryCount: 0,
					since:    now,
					tryAt:    now,
					peer:     peer,
				}

				newProbeCandidates = append(newProbeCandidates, ctx)

			} else if ctx.peer != peer {
				delete(n.probes, path)

				// endpoint changes.
				setDisable(ctx.peer, path, false)
				setDisable(peer, path, false)

				ctx.tryCount = 0
				ctx.since = now
				ctx.peer = peer
				ctx.id = 0
				ctx.tryAt = ctx.since

				newProbeCandidates = append(newProbeCandidates, ctx)
			}

			n.lastFails.Delete(k)
			return true
		})

		{
			owners := make(map[linkPathKey]*MetaPeer)
			for _, ctx := range n.probes {
				if ctx.peer.IsSelf() {
					// myself. should not happen.
					n.log.Warn("[BUG!] probe self endpoint.")
					delete(n.probes, ctx.path)
					setDisable(ctx.peer, ctx.path, false)
					continue
				}

				// drop probes with changed endpoint.
				peer, found := owners[ctx.path]
				if found {
					if peer != ctx.peer ||
						!peer.healthProbe {
						delete(n.probes, ctx.path)
						setDisable(ctx.peer, ctx.path, false)
						continue
					}
				} else {
					paths := ctx.peer.getLinkPaths(n.Publish.Epoch, n.Publish.Backends)
					for _, path := range paths {
						owners[path.linkPathKey] = ctx.peer
					}
					if _, found = owners[ctx.path]; !found {
						delete(n.probes, ctx.path)
						setDisable(ctx.peer, ctx.path, false)
						continue
					}
				}

				// mark disabled endpoint.
				if isUnhealthyPath(ctx) {
					setDisable(ctx.peer, ctx.path, true)
				}
			}
		}

		// republish endpoints.
		for peer, paths := range disablingLog {
			if len(paths) < 1 {
				continue
			}

			peer.filterLinkPath(func(old []*linkPath) []*linkPath {
				commit := false
				if old == nil {
					return nil
				}
				for _, path := range old {
					disabled, changed := paths[path.linkPathKey]
					if !changed {
						continue
					}
					if disabled != path.Disabled {
						if disabled {
							n.log.Warnf("disabled unhealthy path %v of remote %v", path, peer)
						} else {
							n.log.Infof("enabled healthy path %v of %v", path, peer)
						}
						path.Disabled = disabled
					}
					commit = true
				}

				if !commit {
					return nil
				}
				return old
			})
		}

		// start health probes.
		need := n.ProbeBrust
		if need < 1 {
			need = defaultHealthyCheckProbeBrust
		}

		probeTargets, delayedNewCandidates := n._searchForProbeTargets(need, newProbeCandidates)
		for _, target := range delayedNewCandidates {
			n.probes[target.path] = target // save context.
		}

		if len(probeTargets) < 1 {
			n.log.Debugf("health checking finds no probing link path.")
		} else {
			// send probes.
			for _, target := range probeTargets {
				req := endpointProbeRequest{}

				n.probeCounter++
				req.id = n.probeCounter
				req.isResponse = false

				target.id = req.id
				target.tryAt = now
				target.tryCount++

				n.probes[target.path] = target // save context.

				n.log.Debugf("probe link %v. [request ID = %v]", &target.path, req.id)
				payload := req.Encode()
				n.SendViaEndpoint(proto.MsgTypePing, payload, backend.Endpoint{
					Type: target.path.ty, Endpoint: target.path.local,
				}, target.path.remote)
			}
		}

	}, time.Second*10, 1)
}
