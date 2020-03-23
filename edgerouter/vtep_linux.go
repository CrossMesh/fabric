package edgerouter

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"git.uestc.cn/sunmxt/utt/config"
	logging "github.com/sirupsen/logrus"
	"github.com/songgao/water"
)

var (
	TuntapMultiqueuePossiable = IsPlatformMultiqueueTuntapSupported()
)

func IsPlatformMultiqueueTuntapSupported() bool {
	kernel, major, minor, err := getKernelMainVersion()
	if err != nil {
		logging.Warnf("failed to get kernel version (%v). multiqueue tuntap will not be enabled.", err)
	}
	return joinKernelVersion(kernel, major, minor) >= tuntapMultiqueueMinimumKernalVersionJoined
}

func getKernelMainVersion() (kernel, major, minor uint8, err error) {
	raw, ierr := ioutil.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return 0, 0, 0, ierr
	}
	version := strings.TrimSpace(string(raw))
	if parts := strings.SplitN(version, "-", 2); len(parts) > 0 {
		version = parts[0]
	}
	if read, err := fmt.Sscanf(version, "%d.%d.%d", &kernel, &major, &minor); err != nil {
		return 0, 0, 0, err
	} else if read != 3 {
		return 0, 0, 0, fmt.Errorf("unrecogized kernel version \"%v\"", version)
	}
	return
}

func joinKernelVersion(kernel, major, minor uint8) uint32 {
	return uint32(kernel)<<16 | uint32(major)<<8 | uint32(minor)
}

const tuntapMultiqueueMinimumKernalVersionJoined = uint32(3)<<16 | uint32(8)<<8 | uint32(0)

func (v *virtualTunnelEndpoint) synchronizeSystemPlatformConfig(rw *water.Interface) (err error) {
	ifName := rw.Name()

	if v.hwAddr != nil && v.deviceConfig.DeviceType == water.TAP {
		// configure hardware address.
		if err = exec.Command("ip", "link", "set", ifName, "address", v.hwAddr.String()).Run(); err != nil {
			return
		}
	}

	// link up.
	if err = exec.Command("ip", "link", "set", ifName, "up").Run(); err != nil {
		return
	}
	if v.subnet != nil {
		// add ip.
		exec.Command("ip", "addr", "del", v.subnet.String(), "dev", ifName).Run()
		if err = exec.Command("ip", "addr", "add", v.subnet.String(), "dev", ifName).Run(); err != nil {
			return
		}
		if v.deviceConfig.DeviceType == water.TUN {
			// extra routes for overlay network.
			if v.vnet != nil {
				exec.Command("ip", "route", "del", v.vnet.String(), "dev", ifName).Run()
				if err = exec.Command("ip", "route", "add", v.vnet.String(), "dev", ifName).Run(); err != nil {
					return
				}
			}
		}
	}

	return
}

func (v *virtualTunnelEndpoint) setupTuntapPlatformParameters(cfg *config.Interface, deviceConfig *water.Config) {
	deviceConfig.Name = cfg.Name
	deviceConfig.PlatformSpecificParams.MultiQueue = TuntapMultiqueuePossiable
	if deviceConfig.PlatformSpecificParams.MultiQueue && cfg.Multiqueue {
		v.log.Info("enable multiqueue tuntap.")
		v.multiqueue = true
	}
}
