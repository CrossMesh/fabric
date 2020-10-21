package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/crossmesh/fabric/cmd/pb"
	"github.com/crossmesh/fabric/cmd/version"
	arbit "github.com/sunmxt/arbiter"
	"github.com/urfave/cli/v2"
)

// implementation of command "daemon reload"
func (a *CrossmeshApplication) cliReloadCmdAction(ctx *cli.Context) error {
	conn, client, err := a.GetControlRPCClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	req := pb.ReloadRequest{
		ConfigFilePath: a.ConfigFile,
	}
	var result *pb.Result
	cctx, canceled := context.WithTimeout(context.TODO(), time.Second*30)
	defer canceled()
	if result, err = client.ReloadConfig(cctx, &req); err != nil {
		return cmdError("control rpc got error: %v", err)
	}
	if result == nil {
		return cmdError("control rpc got nil result")
	}
	if result.Type == pb.Result_failed {
		return cmdError("operation failed: %v", result.Message)
	}
	if result.Type != pb.Result_succeeded {
		return cmdError("unsupported message type: %v", result.Type)
	}
	a.log.Info("operation succeeded: ", result.Message)

	return nil
}

// implementation of command "daemon"
func (a *CrossmeshApplication) cliRunDaemonAction(ctx *cli.Context) (err error) {
	version.LogVersion(nil)

	a.lock.Lock()

	// load static configuration first.
	cfg := a.getStaticDaemonConfig(false)
	if cfg == nil {
		return errors.New("cannot load configration")
	}

	a.reconfigurateApps()

	apps := a.apps
	var daemonApps []daemonApplication
	for _, app := range apps {
		daemonApp, isDaemonApp := app.(daemonApplication)
		if !isDaemonApp {
			continue
		}
		daemonApps = append(daemonApps, daemonApp)
	}
	if len(daemonApps) < 1 {
		a.log.Info("no daemon to run.")
		a.lock.Unlock()
		return nil
	}

	a.arbiters.main = arbit.New()
	a.arbiters.main.HookPreStop(func() {
		a.log.Info("shutting down...")
	})
	a.arbiters.main.HookStopped(func() {
		a.log.Info("exiting...")
	})
	defer a.arbiters.main.Shutdown()

	// start control rpc.
	a.arbiters.control = arbit.New()
	a.arbiters.main.Go(func() {
		<-a.arbiters.main.Exit()
		a.arbiters.control.Shutdown()
		a.arbiters.control.Join()
	})
	if err = a.reconfigurateControlRPC(cfg.Control); err != nil {
		a.arbiters.main.Shutdown()
		a.arbiters.main.Join()
		a.lock.Unlock()
		return err
	}

	// start all apps.
	a.arbiters.apps = arbit.NewWithParent(a.arbiters.main)
	for _, app := range daemonApps {
		log := a.log.WithField("app", app.AppName())
		if err = app.Launch(a.arbiters.apps, log, ctx); err != nil {
			a.log.Errorf("failed to start daemon app \"%v\". (err = \"%v\")", app.AppName(), err)
			break
		}
	}
	if err != nil {
		a.arbiters.main.Shutdown()
		a.arbiters.main.Join()
		a.lock.Unlock()
		return err
	}

	a.lock.Unlock()

	// join
	return a.arbiters.main.Arbit()
}
