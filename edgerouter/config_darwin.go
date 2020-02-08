package edgerouter

import (
	"git.uestc.cn/sunmxt/utt/config"
	"github.com/songgao/water"
)

func setupTuntapPlatformParameters(cfg *config.Interface, deviceConfig *water.Config) {
	deviceConfig.Name = cfg.Name
	if deviceConfig.DeviceType == water.TAP {
		// tap supported by tuntaposx driver only.
		deviceConfig.Driver = water.MacOSDriverTunTapOSX
	}
	if deviceConfig.DeviceType == water.TUN {
		deviceConfig.Driver = water.MacOSDriverSystem
	}
}
