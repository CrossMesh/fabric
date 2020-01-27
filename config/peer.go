package config

type Backend struct {
	Encrypt    bool        `json:"encrypt" yaml:"encrypt" default:"true"`
	PSK        string      `json:"psk" yaml:"psk"`
	Type       string      `json:"type" yaml:"type"`
	Parameters interface{} `json:"params" yaml:"params"`
}

type Interface struct {
	Name    string `json:"name" yaml:"name"`
	MAC     string `json:"mac" yaml:"mac"`
	Address string `json:"address" yaml:"address"`
	//Network string `json:"network" yaml:"network"`
}

type Network struct {
	PSK     string     `json:"psk" yaml:"psk"`
	Iface   *Interface `json:"iface" yaml:"iface"`
	Backend []*Backend `json:"backends" yaml:"backends"`
	Mode    string     `json:"mode" yaml:"mode"`
	Region  string     `json:"region" yaml:"region"`
}

type Link struct {
	Net map[string]*Network `json:"link" yaml:"link"`
}
