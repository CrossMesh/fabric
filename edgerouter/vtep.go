package edgerouter

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"git.uestc.cn/sunmxt/utt/config"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
)

var (
	ErrNoAvaliableInterface = errors.New("no avaliable interface")
	ErrVTEPClosed           = errors.New("VTEP is closed")
	ErrVTEPQueueRevoke      = errors.New("VTEP queue is revoked")
)

type vtepQueueLease struct {
	lock sync.RWMutex
	rw   *water.Interface
}

func (l *vtepQueueLease) Tx(proc func(*water.Interface) error) error {
	l.lock.RLock()
	defer l.lock.RUnlock()
	rw := l.rw
	if rw == nil {
		return ErrVTEPQueueRevoke
	}
	return proc(rw)
}

func (l *vtepQueueLease) Revoke() (err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	if err = l.rw.Close(); err != nil {
		return err
	}
	l.rw = nil
	return nil
}

type virtualTunnelEndpoint struct {
	lock sync.RWMutex

	leases     []*vtepQueueLease
	ctr, mask  uint32
	get        func() *vtepQueueLease
	multiqueue bool

	subnet, vnet *net.IPNet
	deviceConfig *water.Config
	hwAddr       net.HardwareAddr

	log *logging.Entry
}

func newVirtualTunnelEndpoint(log *logging.Entry) (v *virtualTunnelEndpoint) {
	if log == nil {
		log = logging.WithField("module", "vtep")
	}
	v = &virtualTunnelEndpoint{
		log:    log,
		leases: make([]*vtepQueueLease, 0, 1),
		ctr:    0xFFFFFFFF,
	}
	v.updateGetter()

	return v
}

func (v *virtualTunnelEndpoint) SynchronizeSystemConfig() (err error) {
	v.lock.RLock()
	defer v.lock.RUnlock()
	return v.synchronizeSystemConfig()
}

func (v *virtualTunnelEndpoint) QueueLease() (*vtepQueueLease, error) {
	v.lock.RLock()
	defer v.lock.RUnlock()
	return v.queueLease()
}

func (v *virtualTunnelEndpoint) queueLease() (lease *vtepQueueLease, err error) {
	if v.leases == nil {
		return nil, ErrVTEPClosed
	}
	lease = v.get()
	if lease == nil {
		return nil, ErrVTEPClosed
	}
	// revoked lease. reopen.
	if lease.rw == nil {
		if lease.rw, err = water.New(*v.deviceConfig); err != nil {
			return nil, err
		}
	}
	return
}

func (v *virtualTunnelEndpoint) SetMaxQueue(n uint32) (err error) {
	if n < 1 || !v.multiqueue {
		n = 1
	}
	v.lock.Lock()
	defer v.lock.Unlock()
	if n == uint32(len(v.leases)) {
		return nil
	}

	if n < uint32(len(v.leases)) {
		// less queues.
		for n < uint32(len(v.leases)) {
			idx := len(v.leases) - 1
			// revoke.
			if err := v.leases[idx].Revoke(); err != nil {
				panic(fmt.Sprintf("SetMaxQueue cannot revoke queue lease: %v", err))
			}
		}
		v.updateGetter()

	} else {
		// more queues.
		var rws []*water.Interface
		for more := n - uint32(len(v.leases)); more > 0; more-- {
			rw, ierr := water.New(*v.deviceConfig)
			if ierr != nil {
				err = ierr
				break
			}
			rws = append(rws, rw)
		}
		if err != nil {
			v.log.Error("SetMaxQueue() got failure at water.New(): ", err)
			for _, rw := range rws {
				if err := rw.Close(); err != nil {
					panic(fmt.Sprintf("cannot close water.Interface: %v", err))
				}
			}
		} else {
			// add leases.
			for _, rw := range rws {
				v.leases = append(v.leases, &vtepQueueLease{rw: rw})
			}
			v.updateGetter()
		}
	}

	return
}

func (v *virtualTunnelEndpoint) updateLeases(cfg *water.Config) (err error) {
	if len(v.leases) < 1 {
		// no existings. add one.
		rw, ierr := water.New(*cfg)
		if ierr != nil {
			return ierr
		}
		v.leases = append(v.leases, &vtepQueueLease{
			rw: rw,
		})
		v.updateGetter()
		return
	}

	if len(v.leases) == 1 && !v.multiqueue {
		// normal update.
		lease := v.leases[0]
		lease.lock.Lock()
		if err = lease.rw.Close(); err != nil {
			return err
		}
		if lease.rw, err = water.New(*cfg); err != nil {
			return err
		}
		lease.lock.Unlock()
		return
	}

	// rolling updates.
	newLeases := make([]*water.Interface, len(v.leases))
	for idx := range newLeases {
		newLeases[idx], err = water.New(*cfg)
		if err != nil {
			break
		}
	}
	if err != nil {
		// failure. clean up.
		for idx := range newLeases {
			rw := newLeases[idx]
			if rw != nil {
				if err := rw.Close(); err != nil {
					// no way to recover from this. let it crash.
					panic(fmt.Sprintf("cannot close water.Interface: %v", err))
				}
			}
		}
		return err
	}
	// update the old.
	for idx, lease := range v.leases {
		new := newLeases[idx]
		lease.lock.Lock()
		old := lease.rw
		lease.rw = new
		lease.lock.Unlock()
		if err := old.Close(); err != nil {
			// no way to recover from this. let it crash.
			panic(fmt.Sprintf("cannot close water.Interface: %v", err))
		}
		idx++
	}
	return nil
}

func (v *virtualTunnelEndpoint) getLeaseNil() *vtepQueueLease    { return nil }
func (v *virtualTunnelEndpoint) getLeaseSingle() *vtepQueueLease { return v.leases[0] }
func (v *virtualTunnelEndpoint) getLeaseSlow() *vtepQueueLease {
	return v.leases[atomic.AddUint32(&v.ctr, 1)%uint32(len(v.leases))]
}
func (v *virtualTunnelEndpoint) getLeaseFast() *vtepQueueLease {
	return v.leases[atomic.AddUint32(&v.ctr, 1)&v.mask]
}

func (v *virtualTunnelEndpoint) ApplyConfig(mode string, cfg *config.Interface) (err error) {
	if cfg == nil {
		return errors.New("empty interface configration")
	}
	if v.leases == nil {
		return ErrVTEPClosed
	}

	var (
		ip           net.IP
		subnet, vnet *net.IPNet
		hwAddr       net.HardwareAddr
		deviceConfig water.Config
	)

	switch mode {
	case "ethernet":
		deviceConfig.DeviceType = water.TAP
	case "overlay":
		deviceConfig.DeviceType = water.TUN
	default:
		return ErrUnknownMode
	}

	if cfg.Subnet != "" {
		if ip, subnet, err = net.ParseCIDR(cfg.Subnet); err != nil {
			return err
		}
		subnet.IP = ip
		if cfg.Network != "" && deviceConfig.DeviceType == water.TUN {
			if _, vnet, err = net.ParseCIDR(cfg.Network); err != nil {
				return err
			}
		}
	}
	if cfg.MAC != "" {
		if hwAddr, err = net.ParseMAC(cfg.MAC); err != nil {
			return err
		}
	}
	v.setupTuntapPlatformParameters(cfg, &deviceConfig)

	v.lock.Lock()
	defer v.lock.Unlock()

	if err = v.updateLeases(&deviceConfig); err != nil {
		return err
	}
	v.subnet, v.vnet = subnet, vnet
	v.hwAddr = hwAddr
	v.deviceConfig = &deviceConfig

	return v.synchronizeSystemConfig()
}

func (v *virtualTunnelEndpoint) synchronizeSystemConfig() error {
	lease, err := v.queueLease()
	if err != nil {
		return err
	}
	if lease == nil {
		return ErrNoAvaliableInterface
	}
	if err = lease.Tx(func(rw *water.Interface) error { return v.synchronizeSystemPlatformConfig(rw) }); err != nil {
		return err
	}
	return nil
}

func (v *virtualTunnelEndpoint) updateGetter() {
	if cnt := uint32(len(v.leases)); cnt < 1 {
		v.get = v.getLeaseNil
	} else if cnt == 1 {
		v.get = v.getLeaseSingle
	} else if mask := cnt - 1; cnt&mask == 0 {
		v.get, v.mask = v.getLeaseFast, mask
	} else {
		v.get = v.getLeaseSlow
	}
}

func (v *virtualTunnelEndpoint) Close() (err error) {
	v.lock.Lock()
	defer v.lock.Unlock()

	idx := len(v.leases)
	for idx > 0 {
		idx--
		lease := v.leases[idx]
		if lease.rw != nil {
			if err = lease.rw.Close(); err != nil {
				return err
			}
		}
		lease.rw = nil
		v.leases = v.leases[:idx]
	}
	v.leases = nil
	return
}
