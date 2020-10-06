package metanet

import (
	"container/heap"
	"encoding/binary"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/proto"
)

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

func isUnhealthyEndpoint(ctx *endpointProbingContext) bool {
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

	ctx, _ := n.probes[msg.Endpoint]
	if ctx == nil || ctx.id != header.id {
		return
	}
	delete(n.probes, msg.Endpoint)

	n.recentSuccesses[msg.Endpoint] = &lastProbingContext{
		endpoint: msg.Endpoint,
		id:       header.id,
		peer:     ctx.peer,
		at:       time.Now(),
	}

	if ctx.tryCount > 3 {
		ctx.peer.publishInterval(func(interval *MetaPeerStatePublication) (commit bool) {
			commit = false

			newEndpoints := make([]*MetaPeerEndpoint, 0, len(ctx.peer.Endpoints))
			newEndpoints = append(newEndpoints, ctx.peer.Endpoints...)
			for _, endpoint := range newEndpoints {
				if endpoint.Endpoint == msg.Endpoint && endpoint.Disabled {
					endpoint.Disabled = false
					n.log.Infof("enabled healthy endpoint %v of %v", endpoint.Endpoint, ctx.peer)
				}
			}

			if commit {
				sort.Slice(newEndpoints, func(i, j int) bool {
					return newEndpoints[i].Higher(*newEndpoints[j])
				})
				interval.Endpoints = newEndpoints
			}

			return
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

	type endpointPeer struct {
		endpoint backend.Endpoint
		peer     *MetaPeer
	}

	matchSet := map[endpointPeer]*endpointProbingContext{}

	probesMatchers := []func(*endpointProbingContext) bool{
		func(ctx *endpointProbingContext) bool { return ctx.tryCount == 0 },
		func(ctx *endpointProbingContext) bool {
			return now.Sub(ctx.tryAt) >= n.ProbeTimeout
		},
		func(ctx *endpointProbingContext) bool { return isUnhealthyEndpoint(ctx) },
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

				matchSet[endpointPeer{
					endpoint: ctx.endpoint,
					peer:     ctx.peer,
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
				if peer.IsSelf() {
					continue
				}

				endpoints := peer.Endpoints
				for _, endpoint := range endpoints {

					latestProbingCtx, _ := n.recentSuccesses[endpoint.Endpoint]
					if latestProbingCtx != nil {
						if latestProbingCtx.peer != peer {
							latestProbingCtx.peer = peer
							latestProbingCtx.id = 0
							delete(n.recentSuccesses, endpoint.Endpoint)
						}
					} else if ctx, _ := n.probes[endpoint.Endpoint]; ctx != nil {
						latestProbingCtx = &lastProbingContext{
							endpoint: endpoint.Endpoint,
							id:       ctx.id,
							peer:     peer,
							at:       ctx.tryAt,
						}
					} else {
						latestProbingCtx = &lastProbingContext{
							endpoint: endpoint.Endpoint,
							id:       0,
							peer:     peer,
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
				ctx, _ := n.probes[latestProbingCtx.endpoint]
				if ctx == nil {
					ctx = &endpointProbingContext{
						endpoint: latestProbingCtx.endpoint,
						id:       0,
						tryCount: 0,
						since:    now,
						tryAt:    now,
						peer:     latestProbingCtx.peer,
					}
				}
				matchSet[endpointPeer{
					endpoint: latestProbingCtx.endpoint,
					peer:     latestProbingCtx.peer,
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
		disablingLog := map[*MetaPeer]map[backend.Endpoint]bool{}

		setDisable := func(peer *MetaPeer, endpoint backend.Endpoint, disabled bool) {
			endpoints, _ := disablingLog[peer]
			if endpoints == nil {
				endpoints = make(map[backend.Endpoint]bool)
				disablingLog[peer] = endpoints
			}
			endpoints[endpoint] = disabled
		}

		// deal with recent failures.
		n.lastFails.Range(func(k, v interface{}) bool {
			endpoint, isEndpoint := k.(backend.Endpoint)
			if !isEndpoint {
				n.lastFails.Delete(endpoint)
				return true
			}
			peer, isPeer := v.(*MetaPeer)
			if !isPeer || peer == nil || peer.IsSelf() {
				n.lastFails.Delete(endpoint)
				return true
			}

			ctx, _ := n.probes[endpoint]
			if ctx == nil {
				ctx = &endpointProbingContext{
					endpoint: endpoint,
					tryCount: 0,
					since:    now,
					tryAt:    now,
					peer:     peer,
				}

				newProbeCandidates = append(newProbeCandidates, ctx)

			} else if ctx.peer != peer {
				delete(n.probes, endpoint)

				// endpoint changes.
				setDisable(ctx.peer, endpoint, false)
				setDisable(peer, endpoint, false)

				ctx.tryCount = 0
				ctx.since = now
				ctx.peer = peer
				ctx.id = 0
				ctx.tryAt = ctx.since

				newProbeCandidates = append(newProbeCandidates, ctx)
			}

			n.lastFails.Delete(endpoint)
			return true
		})

		{
			owners := make(map[backend.Endpoint]*MetaPeer)
			for _, ctx := range n.probes {
				if ctx.peer.IsSelf() {
					// myself. should not happen.
					n.log.Warn("[BUG!] probe self endpoint.")
					delete(n.probes, ctx.endpoint)
					setDisable(ctx.peer, ctx.endpoint, false)
					continue
				}

				// drop probes with changed endpoint.
				peer, found := owners[ctx.endpoint]
				if found {
					if peer != ctx.peer {
						delete(n.probes, ctx.endpoint)
						setDisable(ctx.peer, ctx.endpoint, false)
						continue
					}
				} else {
					endpoints := ctx.peer.Endpoints
					for _, endpoint := range endpoints {
						owners[endpoint.Endpoint] = ctx.peer
					}
					if _, found = owners[ctx.endpoint]; !found {
						delete(n.probes, ctx.endpoint)
						setDisable(ctx.peer, ctx.endpoint, false)
						continue
					}
				}

				// mark disabled endpoint.
				if isUnhealthyEndpoint(ctx) {
					setDisable(ctx.peer, ctx.endpoint, true)
				}
			}
		}

		// republish endpoints.
		for peer, endpoints := range disablingLog {
			if len(endpoints) < 1 {
				continue
			}
			peer.publishInterval(func(interval *MetaPeerStatePublication) (commit bool) {
				commit = false

				newEndpoints := make([]*MetaPeerEndpoint, 0, len(peer.Endpoints))
				newEndpoints = append(newEndpoints, peer.Endpoints...)
				for _, endpoint := range newEndpoints {
					disabled, changed := endpoints[endpoint.Endpoint]
					if changed && endpoint.Disabled != disabled {
						commit = true
						endpoint.Disabled = disabled
						if disabled {
							n.log.Warnf("disabled unhealthy endpoint %v of %v", endpoint.Endpoint, peer)
						} else {
							n.log.Infof("enabled healthy endpoint %v of %v", endpoint.Endpoint, peer)
						}
					}
				}

				if commit {
					sort.Slice(newEndpoints, func(i, j int) bool {
						return newEndpoints[i].Higher(*newEndpoints[j])
					})
					interval.Endpoints = newEndpoints
				}

				return
			})
		}

		// start health probes.
		need := n.ProbeBrust
		if need < 1 {
			need = defaultHealthyCheckProbeBrust
		}

		probeTargets, delayedNewCandidates := n._searchForProbeTargets(need, newProbeCandidates)
		for _, target := range delayedNewCandidates {
			n.probes[target.endpoint] = target // save context.
		}

		if len(probeTargets) < 1 {
			n.log.Debugf("health checking finds no probing endpoint.")
		} else {
			// send probes.
			for _, target := range probeTargets {
				req := endpointProbeRequest{}
				req.id = atomic.AddUint64(&n.probeCounter, 1)
				req.isResponse = false

				target.id = req.id
				target.tryAt = now
				target.tryCount++

				n.probes[target.endpoint] = target // save context.

				n.log.Debugf("send probe to %v. [request ID = %v]", target.endpoint, req.id)
				payload := req.Encode()
				n.SendToEndpoints(proto.MsgTypePing, payload, target.endpoint)
			}
		}

	}, time.Second*10, 1)
}
