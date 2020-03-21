package config

import (
	"reflect"
	"runtime"
)

// Backend contains general backend configuration.
type Backend struct {
	// whether encryption enabled.
	Encrypt bool `json:"encrypt" yaml:"encrypt" default:"false"`

	// pre-shared key.
	PSK string `json:"psk" yaml:"psk"`

	// backend engine.
	Type string `json:"type" yaml:"type"`

	// max forward routines.
	MaxConcurrency *uint `json:"-" yaml:"-"`

	// backend specific configurations.
	Parameters map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
}

func (c *Backend) Equal(x *Backend) bool {
	if c == x {
		return true
	}
	var maxConcurrencyX, maxConcurrencyY uint
	if c != nil && c.MaxConcurrency != nil {
		maxConcurrencyX = *c.MaxConcurrency
	}
	if x != nil && x.MaxConcurrency != nil {
		maxConcurrencyY = *x.MaxConcurrency
	}
	if maxConcurrencyX != maxConcurrencyY {
		return false
	}
	return reflect.DeepEqual(c, x)
}

// Interface contains netlink configuraion for network.
type Interface struct {
	// interface name.
	Name string `json:"name" yaml:"name"`

	// (ethernet only) ethernet hardware address.
	MAC string `json:"mac" yaml:"mac"`

	// subnet CIDR representing l3 peer address over virtual network.
	Subnet string `json:"address" yaml:"address"`

	// network CIDR representing whole virtual network.
	Network string `json:"network" yaml:"network"`
}

func (c *Interface) Equal(x *Interface) bool { return reflect.DeepEqual(c, x) }

// Network contains parameters of virtual network.
type Network struct {
	PSK     string     `json:"psk" yaml:"psk"`
	Iface   *Interface `json:"iface" yaml:"iface"`
	Backend []*Backend `json:"backends" yaml:"backends"`
	Mode    string     `json:"mode" yaml:"mode"`

	Region        string `json:"region" yaml:"region"`
	MinRegionPeer int    `json:"minRegionPeer" yaml:"minRegionPeer"`

	MaxConcurrency uint `json:"maxConcurrency" yaml:"maxConcurrency" default:"8"`
}

func (c *Network) Equal(x *Network) (e bool) {
	if c == nil {
		if x == nil {
			return true
		}
		return false
	} else if x == nil {
		return false
	}
	if e = c.PSK == x.PSK && c.Mode == x.Mode &&
		c.Region == x.Region &&
		c.MinRegionPeer == x.MinRegionPeer &&
		c.MaxConcurrency == x.MaxConcurrency; !e {
		return
	}
	if c.Iface != x.Iface {
		e = c.Iface.Equal(x.Iface)
	}

	return reflect.DeepEqual(c.Backend, x.Backend)
}

// ControlRPC contains configuration of control port.
type ControlRPC struct {
	Type     string `json:"type" yaml:"type" default:"unix"`
	Endpoint string `json:"endpoint" yaml:"endpoint" default:"/var/run/utt_control.sock"`
}

func (c *ControlRPC) Equal(x *ControlRPC) bool { return reflect.DeepEqual(c, x) }

// Daemon contains UTT daemon configuration.
type Daemon struct {
	Control *ControlRPC         `json:"control" yaml:"control" default:"{}"`
	Net     map[string]*Network `json:"link" yaml:"link"`
}

func (c *Daemon) Equal(x *Daemon) (e bool) {
	if e = c.Control.Equal(x.Control); !e {
		return false
	}
	if len(c.Net) != len(x.Net) {
		return false
	}
	for name, net := range c.Net {
		xnet, has := x.Net[name]
		if !has {
			return false
		}
		if e = net.Equal(xnet); !e {
			return false
		}
	}
	return true
}

func GetMaxForwardRoutines(suggested uint) uint {
	if suggested < 1 {
		suggested = 1
	}
	ncpu := runtime.NumCPU()
	if ncpu < 1 {
		ncpu = 1
	}
	if uint(ncpu) < suggested {
		suggested = uint(ncpu)
	}
	return suggested
}
