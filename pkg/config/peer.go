package config

type Endpoint struct {
	Address string `json:"address" yaml:"address"`
	Port    uint16 `json:"port" yaml:"port"`
}

type ACL struct {
	PSK     string    `json:"psk" yaml:"psk"`
	Reverse *Endpoint `json:"reverse" yaml:"reverse"`
}

type Peer struct {
	Tunnel Endpoint        `json:"tunnel" yaml:"tunnel"`
	ACL    map[string]*ACL `json:"acl" yaml:"acl"`
}

type UUT struct {
	Peer map[string]*Peer `json:"peer" yaml:"peer"`
}
