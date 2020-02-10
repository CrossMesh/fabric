package edgerouter

import (
	"net"
	"os/exec"

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

func upConfigVTEP(mode string, ifName string, hwAddr net.HardwareAddr, subnet, vnet *net.IPNet) (err error) {
	// parse ip.
	if subnet != nil {
		// add ip.
		if err = exec.Command("ifconfig", ifName, "add", subnet.String(), subnet.IP.String()).Run(); err != nil {
			return err
		}
		if mode == "overlay" {
			// extra routes for overlay network.
			if vnet != nil {
				if err = exec.Command("route", "add", vnet.String(), "-interface", ifName).Run(); err != nil {
					return err
				}
			} else {
				if err = exec.Command("route", "add", subnet.String(), "-interface", ifName).Run(); err != nil {
					return err
				}
			}
		}
	}

	if hwAddr != nil && mode == "ethernet" {
		// configure hardware address.
		if err = exec.Command("ifconfig", ifName, "ether", hwAddr.String()).Run(); err != nil {
			return err
		}
	}

	return nil
}
