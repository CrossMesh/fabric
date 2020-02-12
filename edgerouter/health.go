package edgerouter

import (
	"context"
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
)

// Ping handles Ping RPC request from remote.
func (r *EdgeRouter) Ping(msg *pb.Ping) (*pb.Ping, error) {
	return msg, nil
}

// Ping makes Ping RPC request.
func (c *RPCClient) Ping(ctx context.Context, msg *pb.Ping) (*pb.Ping, error) {
	raw, err := c.Call(ctx, "Ping", msg, c.send)
	if err != nil {
		return nil, err
	}
	resp, validType := raw.(*pb.Ping)
	if !validType {
		return nil, ErrInvalidRPCMessageType
	}
	return resp, nil
}

func (r *EdgeRouter) goBackendHealthCheckOnce(p route.MembershipPeer) {
	determine := func(id backend.PeerBackendIdentity, fail bool) (disable bool) {
		if !fail {
			// clear "failure" flag.
			r.endpointFailures.Delete(id)
			return false
		}
		disable = true
		var lastFailure time.Time

		now := time.Now()
		if v, loaded := r.endpointFailures.Load(id); loaded {
			if lastFailure, loaded = v.(time.Time); !loaded {
				disable = false
			}
		} else {
			disable = false
		}
		if !disable {
			r.endpointFailures.Store(id, now)
			return
		}
		return lastFailure.Add(time.Second * 30).After(now)
	}

	r.routeArbiter.Go(func() {
		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*15)
		defer cancel()
		updated := false
		ping := func(wg *sync.WaitGroup, b *route.PeerBackend) {
			c := r.RPCClientViaBackend(b)
			resp, err := c.Ping(ctx, &pb.Ping{
				Echo: "ping",
			})
			if target := determine(b.PeerBackendIdentity, err != nil || resp == nil); target != b.Disabled {
				updated = true
				b.Disabled = target
				if b.Disabled {
					r.log.Warnf("health check disable %v:%v of %v", b.PeerBackendIdentity.Type, b.PeerBackendIdentity.Endpoint, p)
				} else {
					r.log.Infof("health check enable %v:%v of %v", b.PeerBackendIdentity.Type, b.PeerBackendIdentity.Endpoint, p)
				}
			}
			wg.Done()
		}

		var wg sync.WaitGroup
		for _, b := range p.Backends() {
			wg.Add(1)
			ping(&wg, b)
		}
		wg.Wait()
		if updated {
			p.Meta().Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
				tx.UpdateActiveBackend()
				return true
			})
		}
	})
}
