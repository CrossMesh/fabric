package edgerouter

import (
	"git.uestc.cn/sunmxt/utt/config"
	"github.com/songgao/water"
)

func setupTuntapPlatformParameters(cfg *config.Interface, deviceConfig *water.Config) {
	deviceConfig.Name = cfg.Name
}
