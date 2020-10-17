package edgerouter

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/config"
	"github.com/crossmesh/fabric/route"
	arbit "github.com/sunmxt/arbiter"
)

func (r *EdgeRouter) updateBackends(cfgs []*config.Backend) (succeed bool) {
	var err error

	creators := make([]backend.BackendCreator, 0, len(cfgs))
	for _, cfg := range cfgs {
		var creator backend.BackendCreator
		if cfg == nil {
			continue
		}
		if creator, err = backend.GetCreator(cfg.Type, cfg); err != nil {
			if err == backend.ErrBackendTypeUnknown {
				r.log.Errorf("unknown backend type \"%v\"", cfg.Type)
			} else {
				r.log.Errorf("failed to get backend creator. (err = \"%v\")", err)
			}
			continue
		}
		creators = append(creators, creator)
	}

	if len(creators) < 1 {
		r.log.Errorf("no valid backend configure. reject configuration changes.")
		return false
	}

	r.metaNet.UpdateLocalEndpoints(creators...)

	return true
}

func (r *EdgeRouter) goApplyConfig(cfg *config.Network, cidr string) {
	id, log := atomic.AddUint32(&r.configID, 1), r.log.WithField("type", "config")

	log.Infof("start apply configuration %v", id)

	r.arbiters.config.Go(func() {
		var err error

		succeed, rebootForward, forwardRoutines := true, false, cfg.GetMaxConcurrency()
		r.lock.Lock()
		defer r.lock.Unlock()

		for r.arbiters.config.ShouldRun() {
			if !succeed {
				log.Info("retry at 15 seconds.")
				time.Sleep(15 * time.Second)
				succeed = true
			}

			if thisID := r.configID; thisID != id {
				break
			}
			updateVTEP := false

			// quiting timeout for gossip.
			quitTimeout := time.Duration(0)
			if ref := cfg.QuitTimeout; ref != nil {
				quitTimeout = time.Duration(*ref) * time.Second
				r.metaNet.SetGossipQuitTimeout(quitTimeout)
			} else {
				quitTimeout = r.metaNet.GetGossipQuitTimeout()
			}
			r.log.Info("gossip quit timeout = ", quitTimeout)

			// update peer and route.
			if current := r.Mode(); current != "unknown" && cfg.Mode != current {
				r.log.Info("shutting down forwarding...")
				if arbiter := r.arbiters.forward; arbiter != nil {
					arbiter.Shutdown()
					arbiter.Join()
					r.arbiters.forward = nil
					r.route = nil
				}
				updateVTEP = true
			}
			if r.arbiters.forward == nil {
				rebootForward = true
				r.arbiters.forward = arbit.NewWithParent(r.arbiters.main)
			}

			// create route.
			if r.route == nil {
				log.Debug("starting new forwarding...")

				switch cfg.Mode {
				case "ethernet":
					log.Info("network mode: ethernet")
					r.route = route.NewP2PL2MeshNetworkRouter()

				case "ip":
					log.Info("network mode: ip")
					r.route = route.NewP2PL3IPv4MeshNetworkRouter()
				}

			}

			// update vetp.
			if updateVTEP || r.cfg == nil || !cfg.Iface.Equal(r.cfg.Iface) {
				if err = r.vtep.ApplyConfig(cfg.Mode, cfg.Iface); err != nil {
					log.Error("update VTEP failure: ", err)
					succeed = false
					continue
				}
				r.vtep.SetMaxQueue(uint32(forwardRoutines))
			}

			if old, err := r.metaNet.SetRegion(cfg.Region); err != nil {
				log.Error("cannot set region for metadata network. (err = \"%v\")", err)
				succeed = false
				continue
			} else if old != cfg.Region {
				log.Infof("metanet region: \"%v\" --> \"%v\"", old, cfg.Region)
			}

			// update backends.
			if succeed = r.updateBackends(cfg.Backend); !succeed {
				continue
			}

			minRegionPeer := cfg.MinRegionPeer
			if minRegionPeer < 1 {
				minRegionPeer = 2
			}
			log.Infof("minimum region peer = %v", minRegionPeer)
			r.metaNet.SetMinRegionPeer(uint(minRegionPeer))

			break
		}

		if succeed {
			r.cfg = cfg

			if rebootForward {
				r.rebuildRoute(false)

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
	if cfg.Mode != "ethernet" && cfg.Mode != "overlay" && cfg.Mode != "ip" {
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
	case "overlay":
		r.log.Warn("network mode \"overlay\" is now renamed \"ip\". ")
		cfg.Mode = "ip"
	}

	r.goApplyConfig(cfg, cfg.Iface.Subnet)

	return nil
}
