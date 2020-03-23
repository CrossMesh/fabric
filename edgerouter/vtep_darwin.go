package edgerouter

import (
	"os/exec"

	"git.uestc.cn/sunmxt/utt/config"
	"github.com/songgao/water"
)

const (
	TuntapMultiqueuePossiable = false
)

func (v *virtualTunnelEndpoint) setupTuntapPlatformParameters(cfg *config.Interface, deviceConfig *water.Config) {
	deviceConfig.Name = cfg.Name
	if deviceConfig.DeviceType == water.TAP {
		// tap supported by tuntaposx driver only.
		deviceConfig.Driver = water.MacOSDriverTunTapOSX
	}
	if deviceConfig.DeviceType == water.TUN {
		deviceConfig.Driver = water.MacOSDriverSystem
	}
	if cfg.GetMultiqueue() {
		v.log.Warn("no multiqueue tuntap supported for MacOS yet")
	}
}

func (v *virtualTunnelEndpoint) synchronizeSystemPlatformConfig(rw *water.Interface) (err error) {
	ifName := rw.Name()

	// parse ip.
	if v.subnet != nil {
		// should remove first.
		exec.Command("ifconfig", ifName, "del", v.subnet.String(), v.subnet.IP.String()).Run()
		// add ip.
		if err = exec.Command("ifconfig", ifName, "add", v.subnet.String(), v.subnet.IP.String()).Run(); err != nil {
			return err
		}
		if v.deviceConfig.DeviceType == water.TUN {
			// extra routes for overlay network.
			if v.vnet != nil {
				exec.Command("route", "del", v.vnet.String(), "-interface", ifName).Run()
				if err = exec.Command("route", "add", v.vnet.String(), "-interface", ifName).Run(); err != nil {
					return err
				}
			} else {
				exec.Command("route", "del", v.subnet.String(), "-interface", ifName).Run()
				if err = exec.Command("route", "add", v.subnet.String(), "-interface", ifName).Run(); err != nil {
					return err
				}
			}
		}
	}

	if v.hwAddr != nil && v.deviceConfig.DeviceType == water.TAP {
		// configure hardware address.
		if err = exec.Command("ifconfig", ifName, "ether", v.hwAddr.String()).Run(); err != nil {
			return err
		}
	}
	return nil
}
