package edgerouter

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/backend"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/route"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
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
		}
		return true
	})

	// new.
	for index, creator := range backendCreators {
		var new backend.Backend
		_, hasBackend := r.backends.Load(index)
		if !hasBackend {
			if new, err = creator.New(r.arbiter, nil); err != nil {
				r.log.Errorf("create backend %v:%v failure: %v", backend.GetBackendIdentityName(creator.Type()), creator.Publish, err)
			} else {
				r.backends.Store(index, new)
				new.Watch(r.receiveRemote)
			}
		}
		delete(backendCreators, index)
	}
	// update.
	for index, creator := range backendCreators {
		if backend := r.getBackend(index); backend != nil {
			backend.Shutdown()
			r.backends.Delete(index)
		}
		var new backend.Backend
		if new, err = creator.New(r.arbiter, nil); err != nil {
			r.log.Errorf("create backend %v:%v failure: %v", backend.GetBackendIdentityName(creator.Type()), creator.Publish, err)
		} else {
			r.backends.Store(index, new)
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
	id := atomic.AddUint32(&r.configID, 1)

	r.arbiter.Go(func() {
		var (
			deviceConfig water.Config
			nextTry      time.Time
			err          error
		)

		succeed := false
		for r.arbiter.ShouldRun() && !succeed {
			if nextTry.After(time.Now()) {
				time.Sleep(time.Second)
				continue
			}
			succeed = true

			r.lock.Lock()
			defer r.lock.Unlock()
			if thisID := r.configID; thisID != id {
				break
			}

			// update peer and route.
			if current := r.Mode(); current != "unknown" && cfg.Mode != current {
				r.routeArbiter.Shutdown()
				r.forwardArbiter.Shutdown()
				r.routeArbiter.Join()
				r.forwardArbiter.Join()
				r.ifaceDevice.Close()
				r.routeArbiter, r.forwardArbiter, r.route, r.peerSelf, r.ifaceDevice = nil, nil, nil, nil, nil
			}
			if r.routeArbiter == nil {
				r.routeArbiter = arbit.NewWithParent(r.arbiter, nil)
			}
			if r.forwardArbiter == nil {
				r.forwardArbiter = arbit.NewWithParent(r.arbiter, logging.WithField("module", "forward"))
			}
			// only gossip membership supported yet.
			if r.membership == nil {
				r.membership = route.NewGossipMembership()
			}
			if r.route == nil {
				deviceConfig.PlatformSpecificParams.Name = cfg.Iface.Name

				switch cfg.Mode {
				case "ethernet":
					r.route = route.NewL2Router(r.routeArbiter, r.membership, nil, time.Second*180)
					r.peerSelf = &route.L2Peer{}
					deviceConfig.PlatformSpecificParams.Driver = water.TAP

				case "overlay":
					r.route = route.NewL3Router(r.routeArbiter, r.membership, nil, time.Second*180)
					l3 := &route.L3Peer{}
					r.peerSelf = l3
					deviceConfig.PlatformSpecificParams.Driver = water.TUN
					l3.Tx(func(p route.MembershipPeer, tx *route.L3PeerReleaseTx) bool {
						if err = tx.CIDR(cidr); err != nil {
							return false
						}
						return true
					})
				}
				if err != nil {
					r.log.Error("create netlink failure: ", err)
					succeed = false
					continue
				}

				r.peerSelf.Meta().Tx(func(p route.MembershipPeer, tx *route.PeerReleaseTx) bool {
					tx.Region(r.cfg.Region)
					tx.ClaimAlive()
					return true
				})

				if r.ifaceDevice, err = water.New(deviceConfig); err != nil {
					r.log.Error("interface create failure: ", err)
					succeed = false
				}
			}

			if err = r.updateBackends(cfg.Backend); err != nil {
				r.log.Error("update backend failure: ", err)
				succeed = false
			}

			if !succeed {
				nextTry = time.Now().Add(15 * time.Second)
			}
		}

		if succeed {
			// seed myself.
			r.goMembership()

			// start forward.
			r.forwardArbiter.Go(func() { r.forwardVTEP() })

			// start gossip.
			//a.goGossip(30 * time.Second)

			r.log.Info("new config applied.")
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
	var (
	//ifaceMAC net.HardwareAddr
	)
	switch cfg.Mode {
	case "ethernet":
		if _, err = net.ParseMAC(cfg.Iface.MAC); err != nil {
			r.log.Error("invalid hardware address: ", cfg.Iface.MAC)
			return err
		}
	}

	r.goApplyConfig(cfg, cfg.Iface.Address)

	return nil
}
