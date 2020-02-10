package edgerouter

import (
	"net"
	"os/exec"

	"git.uestc.cn/sunmxt/utt/config"
	"github.com/songgao/water"
)

func setupTuntapPlatformParameters(cfg *config.Interface, deviceConfig *water.Config) {
	deviceConfig.Name = cfg.Name
}

func upConfigVTEP(mode string, ifName string, hwAddr net.HardwareAddr, subnet, vnet *net.IPNet) (err error) {
	if hwAddr != nil && mode == "ethernet" {
		// configure hardware address.
		if err = exec.Command("ip", "link", "set", ifName, "address", hwAddr.String()).Run(); err != nil {
			return err
		}
	}

	// link up.
	if err = exec.Command("ip", "link", "set", ifName, "up").Run(); err != nil {
		return err
	}
	if subnet != nil {
		// add ip.
		if err = exec.Command("ip", "addr", "add", subnet.String(), "dev", ifName).Run(); err != nil {
			return err
		}
		if mode == "overlay" {
			// extra routes for overlay network.
			if vnet != nil {
				if err = exec.Command("ip", "route", "add", vnet.String(), "dev", ifName).Run(); err != nil {
					return err
				}
			}
		}
	}
	return
}
