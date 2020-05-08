package edgerouter

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/route"
	arbit "github.com/sunmxt/arbiter"
)

func (r *EdgeRouter) updateBackends(cfgs []*config.Backend) (err error) {
	backendCreators := make(map[backend.PeerBackendIdentity]backend.BackendCreator, len(cfgs))
	for _, cfg := range cfgs {
		var creator backend.BackendCreator
		if cfg == nil {
			continue
		}
		if creator, err = backend.GetCreator(cfg.Type, cfg); err != nil {
			if err == backend.ErrBackendTypeUnknown {
				return fmt.Errorf("unknown backend type \"%v\"", cfg.Type)
			}
			return err
		}
		if creator == nil {
			return errors.New("got nil backend creator")
		}
		backendCreators[backend.PeerBackendIdentity{
			Type:     creator.Type(),
			Endpoint: creator.Publish(),
		}] = creator
	}

	// shutdown.
	r.visitBackends(func(index backend.PeerBackendIdentity, b backend.Backend) bool {
		if _, hasBackend := backendCreators[index]; !hasBackend {
			b.Shutdown()
			r.deleteBackend(index)
		}
		return true
	})

	for index, creator := range backendCreators {
		var new backend.Backend
		b := r.getBackend(index)
		if b != nil {
			// update
			b.Shutdown()
			r.deleteBackend(index)
		}
		// new.
		if new, err = creator.New(r.arbiter, nil); err != nil {
			r.log.Errorf("create backend %v:%v failure: %v", backend.GetBackendIdentityName(creator.Type()), creator.Publish, err)
		} else {
			r.storeBackend(index, new)
			new.Watch(r.receiveRemote)
		}
	}

	// publish backends.
	backendID := make([]*route.PeerBackend, 0)
	r.visitBackends(func(id backend.PeerBackendIdentity, b backend.Backend) bool {
		backendID = append(backendID, &route.PeerBackend{
			PeerBackendIdentity: id,
			Priority:            b.Priority(),
			Disabled:            false,
		})
		return true
	})
	r.peerSelf.Meta().Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
		tx.Backend(backendID...)
		return true
	})

	return
}

func (r *EdgeRouter) goApplyConfig(cfg *config.Network, cidr string) {
	id, log := atomic.AddUint32(&r.configID, 1), r.log.WithField("type", "config")

	log.Debugf("start apply configuration %v", id)

	r.arbiter.Go(func() {
		var (
			nextTry time.Time
			err     error
		)

		succeed, rebootRoute, rebootForward, nextTry, forwardRoutines := false, false, false, time.Now(), cfg.GetMaxConcurrency()
		r.lock.Lock()
		defer r.lock.Unlock()
		for r.arbiter.ShouldRun() && !succeed {
			if time.Now().Before(nextTry) {
				time.Sleep(time.Second)
				continue
			}
			succeed = true

			if thisID := r.configID; thisID != id {
				break
			}
			updateVTEP := false

			// update peer and route.
			if current := r.Mode(); current != "unknown" && cfg.Mode != current {
				r.log.Debug("shutting down forwarding...")
				r.routeArbiter.Shutdown()
				r.routeArbiter.Join()
				r.routeArbiter, r.forwardArbiter, r.route, r.peerSelf, r.membership = nil, nil, nil, nil, nil
				updateVTEP = true
			}
			if r.routeArbiter == nil {
				rebootRoute = true
				r.routeArbiter = arbit.NewWithParent(r.arbiter)
			}
			if r.forwardArbiter == nil {
				rebootForward = true
				r.forwardArbiter = arbit.NewWithParent(r.arbiter)
			}
			// only gossip membership supported yet.
			if r.membership == nil {
				g := route.NewGossipMembership()
				g.New = r.newGossipPeer
				r.membership = g
			}
			if cfg.MinRegionPeer > 0 {
				if g, isGossip := r.membership.(*route.GossipMembership); isGossip && g != nil {
					g.SetMinRegionPeers(cfg.MinRegionPeer)
				}
			}

			// create route.
			if r.route == nil {
				log.Debug("starting new forwarding...")

				switch cfg.Mode {
				case "ethernet":
					log.Info("network mode: ethernet")

					r.route = route.NewL2Router(r.routeArbiter, r.membership, nil, time.Second*180)
					r.peerSelf = &route.L2Peer{}

				case "overlay":
					log.Info("network mode: overlay")

					r.route = route.NewL3Router(r.routeArbiter, r.membership, nil, time.Second*180)
					l3 := &route.L3Peer{}
					r.peerSelf = l3
					l3.Tx(func(p route.MembershipPeer, tx *route.L3PeerReleaseTx) bool {
						if err = tx.CIDR(cidr); err != nil {
							return false
						}
						return true
					})
				}
				if err != nil {
					log.Error("create netlink failure: ", err)
					succeed = false
					continue
				}

				r.peerSelf.Meta().Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
					tx.Region(cfg.Region)
					tx.ClaimAlive()
					return true
				})
			}
			// update vetp.
			if updateVTEP || r.cfg == nil || !cfg.Iface.Equal(r.cfg.Iface) {
				if err = r.vtep.ApplyConfig(r.Mode(), cfg.Iface); err != nil {
					log.Error("update VTEP failure: ", err)
					succeed = false
				}
				r.vtep.SetMaxQueue(uint32(forwardRoutines))
			}

			// update backends.
			if err = r.updateBackends(cfg.Backend); err != nil {
				log.Error("update backend failure: ", err)
				succeed = false
			}

			if !succeed {
				nextTry = time.Now().Add(15 * time.Second)
				log.Info("retry at 15 seconds.")
			}
		}

		if succeed {
			r.cfg = cfg

			if rebootRoute {
				r.goMembership()
			}
			if rebootForward {
				// start forward.
				for n := uint(0); n < forwardRoutines; n++ {
					r.goForwardVTEP()

				}
			}

			r.log.Debug("new config applied.")
		}
	})
}

func (r *EdgeRouter) ApplyConfig(cfg *config.Network) (err error) {
	defer func() {
		if err != nil {
			r.log.Error(err, ".")
		}
	}()

	if len(cfg.Backend) < 1 {
		err = fmt.Errorf("no backend configured")
		return
	}
	if cfg.Iface.Name == "" {
		err = fmt.Errorf("empty network interface name")
		return
	}
	if cfg.Mode != "ethernet" && cfg.Mode != "overlay" {
		err = fmt.Errorf("unknwon network mode: %v", cfg.Mode)
		return
	}

	switch cfg.Mode {
	case "ethernet":
		if cfg.Iface.MAC != "" {
			if _, err = net.ParseMAC(cfg.Iface.MAC); err != nil {
				r.log.Error("invalid hardware address: ", cfg.Iface.MAC)
				return err
			}
		}
	}

	r.goApplyConfig(cfg, cfg.Iface.Subnet)

	return nil
}
