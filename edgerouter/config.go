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

	creators := make([]backend.BackendCreator, len(cfgs))
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

	log.Debugf("start apply configuration %v", id)

	r.arbiter.Go(func() {
		var err error

		succeed, rebootForward, forwardRoutines := true, false, cfg.GetMaxConcurrency()
		r.lock.Lock()
		defer r.lock.Unlock()

		for r.arbiter.ShouldRun() {
			if !succeed {
				log.Info("retry at 15 seconds.")
				time.Sleep(15 * time.Second)
				succeed = true
			}

			if thisID := r.configID; thisID != id {
				break
			}
			updateVTEP := false

			// update peer and route.
			if current := r.Mode(); current != "unknown" && cfg.Mode != current {
				r.log.Debug("shutting down forwarding...")
				if arbiter := r.forwardArbiter; arbiter != nil {
					arbiter.Shutdown()
					arbiter.Join()
					r.forwardArbiter = nil
					r.route = nil
				}
				updateVTEP = true
			}
			if r.forwardArbiter == nil {
				rebootForward = true
				r.forwardArbiter = arbit.NewWithParent(r.arbiter)
			}

			// create route.
			if r.route == nil {
				log.Debug("starting new forwarding...")

				switch cfg.Mode {
				case "ethernet":
					log.Info("network mode: ethernet")
					r.route = route.NewP2PL2MeshNetworkRouter()

				case "overlay":
					log.Info("network mode: overlay")
					r.route = route.NewP2PL3IPv4MeshNetworkRouter()
				}

				r.rebuildRoute(false)
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
			if succeed = r.updateBackends(cfg.Backend); !succeed {
				continue
			}

			break
		}

		if succeed {
			r.cfg = cfg

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
