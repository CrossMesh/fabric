package config

type TCPBackend struct {
	Bind      string      `json:"bind" yaml:"bind"`
	Publish   string      `json:"publish" yaml:"publish"`
	Priority  uint        `json:"priority" yaml:"priority"`
	StartCode interface{} `json:"startCode" yaml:"startCode"`
	Encrypt   bool        `json:"encrypt" yaml:"encrypt" default:"true"`
}

type Network struct {
	PSK     string                 `json:"psk" yaml:"psk"`
	Iface   string                 `json:"interface" yaml:"interface"`
	Backend map[string]interface{} `json:"backends" yaml:"backends"`
	Mode    string                 `json:"mode" yaml:"mode"`
	Tags    []string               `json:"tags" yaml:"tags"`
}
type Link struct {
	Net map[string]*Network `json:"link" yaml:"link"`
}
