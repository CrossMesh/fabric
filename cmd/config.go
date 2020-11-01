package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/crossmesh/fabric/cmd/pb"
	"github.com/jinzhu/configor"
)

// ReloadConfig implements gRPC method "ReloadConfig".
func (a *CrossmeshApplication) ReloadConfig(ctx context.Context, req *pb.ReloadRequest) (*pb.Result, error) {
	if req == nil {
		return resultInvalidRequest, nil
	}
	if len(req.ConfigFilePath) < 1 {
		return &pb.Result{Type: pb.Result_failed, Message: "invalid request: missing config file path."}, nil
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	a.log.Infof("try to reload config \"%v\"", req.ConfigFilePath)
	newCfg := a.loadConfig(req.ConfigFilePath, false)
	if newCfg == nil {
		return &pb.Result{Type: pb.Result_failed, Message: "failed to reload config."}, nil
	}
	if err := a.reconfigurateControlRPC(newCfg.Control); err != nil {
		msg := fmt.Sprintf("failed to load new control RPC config. (err = \"%v\")", err.Error())
		return &pb.Result{Type: pb.Result_failed, Message: msg}, nil
	}
	a.reconfigurateApps()
	return &pb.Result{Type: pb.Result_failed, Message: "successfully preload static config. please check daemon logs for more details."}, nil
}

// GetLegacyConfigration returns configuration by file.
func (a *CrossmeshApplication) getStaticDaemonConfig(noError bool) *daemonConfig {
	if cfg := a.config; cfg != nil {
		return cfg
	}
	return a.loadConfig("", noError)
}

func (a *CrossmeshApplication) loadConfig(path string, noError bool) *daemonConfig {
	if path == "" {
		path = a.ConfigFile
	}
	if path == "" {
		path = "/etc/utt.yml"
	}
	// config file is a must.
	if fileInfo, err := os.Stat(path); err != nil {
		if !noError {
			a.log.Errorf("cannot get stat of configuration file. [path = \"%v\"] (err = \"%v\")", a.ConfigFile, err)
		}
		return nil
	} else if !fileInfo.Mode().IsRegular() {
		if !noError {
			a.log.Errorf("invalid irregular configuration file. [path = \"%v\"]", a.ConfigFile)
		}
		return nil
	}

	cfg := &daemonConfig{}
	if err := configor.New(&configor.Config{
		Debug: false,
	}).Load(cfg, a.ConfigFile); err != nil {
		a.log.Errorf("failed to load configuration. (err = \"%v\")", err)
		return nil
	}
	a.config = cfg
	a.ConfigFile = path

	return cfg
}

func (a *CrossmeshApplication) reconfigurateApps() {
	// configure apps.
	for _, app := range a.apps {
		app, isConfigApp := app.(staticConfigurationAccpeter)
		if !isConfigApp {
			continue
		}
		app.ReloadStaticConfig(a.ConfigFile)
	}
}
