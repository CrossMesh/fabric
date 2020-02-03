package config

import "reflect"

// Backend contains general backend configuration.
type Backend struct {
	// whether encryption enabled.
	Encrypt bool `json:"encrypt" yaml:"encrypt" default:"true"`

	// pre-shared key.
	PSK string `json:"psk" yaml:"psk"`

	// network type. can be "overlay" or "ethernet"
	Type string `json:"type" yaml:"type"`

	// backend specific configurations.
	Parameters map[string]interface{} `json:"params" yaml:"params"`
}

func (c *Backend) Equal(x *Backend) bool {
	return reflect.DeepEqual(c, x)
}

// Interface contains netlink configuraion for network.
type Interface struct {
	// interface name.
	Name string `json:"name" yaml:"name"`

	// ethernet hardware address.
	MAC string `json:"mac" yaml:"mac"`

	// (overlay only) network CIDR representing l3 peer address over virtual network
	// and subnet.
	Subnet string `json:"address" yaml:"address"`

	// (overlay only) network CIDR representing whole virtual network.
	Network string `json:"network" yaml:"network"`
}

func (c *Interface) Equal(x *Interface) bool { return reflect.DeepEqual(c, x) }

// Network contains parameters of virtual network.
type Network struct {
	PSK     string     `json:"psk" yaml:"psk"`
	Iface   *Interface `json:"iface" yaml:"iface"`
	Backend []*Backend `json:"backends" yaml:"backends"`
	Mode    string     `json:"mode" yaml:"mode"`
	Region  string     `json:"region" yaml:"region"`
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
	if e = c.PSK == x.PSK && c.Mode == x.Mode && c.Region == x.Region; !e {
		return
	}
	if c.Iface != x.Iface {
		e = c.Iface.Equal(x.Iface)
	}
	return
}

// ControlRPC contains configuration of control port.
type ControlRPC struct {
	Type     string `json:"type" yaml:"type" default:"unix"`
	Endpoint string `json:"endpoint" yaml:"endpoint" default:"/var/run/utt.sock"`
}

func (c *ControlRPC) Equal(x *ControlRPC) bool { return reflect.DeepEqual(c, x) }

// Daemon contains UTT daemon configuration.
type Daemon struct {
	Control ControlRPC          `json:"clientRPC" yaml:"clientRPC"`
	Net     map[string]*Network `json:"link" yaml:"link"`
}

func (c *Daemon) Equal(x *Daemon) (e bool) {
	if e = c.Control.Equal(&x.Control); !e {
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
