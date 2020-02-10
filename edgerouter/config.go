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
		}
		r.deleteBackend(index)
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

func (r *EdgeRouter) initializeVTEP(mode string, cfg *config.Interface) (succeed bool, err error) {
	if cfg == nil {
		return false, nil
	}
	if r.ifaceDevice != nil {
		return true, nil
	}
	defer func() {
		if err != nil && r.ifaceDevice != nil {
			r.ifaceDevice.Close()
		}
	}()

	var (
		ip           net.IP
		subnet, vnet *net.IPNet
		deviceConfig water.Config
		hwAddr       net.HardwareAddr
	)

	if cfg.Subnet != "" {
		if ip, subnet, err = net.ParseCIDR(cfg.Subnet); err != nil {
			return false, err
		}
		subnet.IP = ip
		if cfg.Network != "" && mode == "overlay" {
			if _, vnet, err = net.ParseCIDR(cfg.Network); err != nil {
				return false, err
			}
		}
	}
	if cfg.MAC != "" {
		if hwAddr, err = net.ParseMAC(cfg.MAC); err != nil {
			return false, err
		}
	}

	switch mode {
	case "ethernet":
		deviceConfig.DeviceType = water.TAP

	case "overlay":
		deviceConfig.DeviceType = water.TUN

	default:
		return false, ErrUnknownMode
	}

	setupTuntapPlatformParameters(cfg, &deviceConfig)

	// create tuntap.
	if r.ifaceDevice, err = water.New(deviceConfig); err != nil {
		r.log.Error("interface create failure: ", err)
	}

	err = upConfigVTEP(mode, r.ifaceDevice.Name(), hwAddr, subnet, vnet)

	return err == nil, err
}

func (r *EdgeRouter) goApplyConfig(cfg *config.Network, cidr string) {
	id, log := atomic.AddUint32(&r.configID, 1), r.log.WithField("type", "config")

	log.Infof("start apply configuration %v", id)

	r.arbiter.Go(func() {
		var (
			nextTry time.Time
			err     error
		)

		succeed, nextTry := false, time.Now()
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

			// update peer and route.
			if current := r.Mode(); current != "unknown" && cfg.Mode != current {
				r.log.Info("shutting down forwarding...")
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
				r.forwardArbiter = arbit.NewWithParent(r.arbiter, r.log.WithField("type", "forward"))
			}
			// only gossip membership supported yet.
			if r.membership == nil {
				g := route.NewGossipMembership()
				g.New = r.newGossipPeer
				r.membership = g
			}
			// create route.
			if r.route == nil {
				log.Info("starting new forwarding...")

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
			// create tuntap.
			if succeed, err = r.initializeVTEP(cfg.Mode, cfg.Iface); err != nil {
				log.Error("interface create failure: ", err)
				succeed = false
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
			r.goMembership()

			// start forward.
			r.forwardArbiter.Go(func() { r.forwardVTEP() })

			r.log.Info("new config applied.")
			r.cfg = cfg
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
