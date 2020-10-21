package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/crossmesh/fabric/backend"
	"github.com/crossmesh/fabric/control"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
	"github.com/urfave/cli/v2"
)

type coreDaemonApplication struct {
	lock sync.RWMutex

	log *logging.Entry

	configPath string

	mgr *control.NetworkManager
}

func newCoreDaemonApplication() *coreDaemonApplication {
	a := &coreDaemonApplication{
		log: logging.WithField("app", "daemon"),
	}

	return a
}

func (a *coreDaemonApplication) newCliApp(out, err io.Writer) (*cli.App, *coreDaemonApplicationCommandContext) {
	ctx := &coreDaemonApplicationCommandContext{
		app: a,
		out: out,
		err: err,
	}
	cli := &cli.App{
		//Name: "crossmesh core daemon",
		Commands: []*cli.Command{
			{
				Name:  "net",
				Usage: "network control.",
				Subcommands: []*cli.Command{
					{
						Name:   "set",
						Usage:  "start/stop network.",
						Action: a.cliRunNetworkSetAction,
						Flags: []cli.Flag{
							&cli.IntFlag{
								Name:        "retry",
								Aliases:     []string{"r"},
								Usage:       "retry times. (-1 for unlimited)",
								Required:    false,
								DefaultText: "0",
								Destination: &ctx.retry,
							},
						},
					},
					{
						Name:   "seed",
						Usage:  "seed gossip peer.",
						Action: a.cliRunSeedAction,
					},
				},
			},
		},
	}
	cli.Writer = out
	cli.ErrWriter = err
	return cli, ctx
}

func (a *coreDaemonApplication) AppName() string {
	return "core_daemon"
}

func (a *coreDaemonApplication) cliRunNetworkSetAction(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	if ctx.Args().Len() < 1 {
		fmt.Fprintln(cmdCtx.err, "network missing.")
		return nil
	}
	if ctx.Args().Len() != 2 {
		return cmdError("cannot understand operation.")
	}

	netName := ctx.Args().Get(0)
	isStart := false
	switch op := strings.ToLower(ctx.Args().Get(1)); op {
	case "up":
		isStart = true
	case "down":
		isStart = false
	default:
		return cmdError("unknown operation \"%v\"", op)
	}
	retry := cmdCtx.retry

	shouldRetry := func() bool {
		if retry == -1 {
			return true
		}
		if retry > 0 {
			retry--
			return true
		}
		return false
	}

	invalidParamsError := cmdError("invalid parameters")
	a.lock.RLock()
	if a.mgr == nil {
		a.lock.RUnlock()
		return cmdError("network manager not started")
	}

	var lastError error
	for {
		if lastError != nil {
			if !shouldRetry() {
				break
			}

			a.lock.RUnlock()
			time.Sleep(time.Second * 3)
			lastError = nil
			a.lock.RLock()
		}

		net := a.mgr.GetNetwork(netName)
		if net == nil {
			fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" not found.")
			lastError = invalidParamsError
			continue
		}

		if isStart {
			lastError = net.Up()
		} else {
			lastError = net.Down()
		}
		if lastError != nil {
			fmt.Fprintln(cmdCtx.err, "operation failed: ", lastError)
			continue
		}

		break
	}

	fmt.Fprintln(cmdCtx.out, "succeeded.")

	a.lock.RUnlock()

	return nil
}

func (a *coreDaemonApplication) cliRunSeedAction(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	invalidParamsError := cmdError("invalid parameters")

	if ctx.Args().Len() < 1 {
		fmt.Fprintln(cmdCtx.err, "network missing.")
		return invalidParamsError
	}
	if ctx.Args().Len() < 2 {
		fmt.Fprintln(cmdCtx.err, "missing active endpoint.")
		return invalidParamsError
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	if a.mgr == nil {
		return cmdError("network manager not started")
	}

	netName := ctx.Args().Get(0)

	var endpoints []backend.Endpoint

	for _, ep := range ctx.Args().Slice()[1:] {
		parts := strings.SplitN(ep, ":", 2)
		if len(parts) < 2 {
			fmt.Fprintln(cmdCtx.err, "endpoint with invalid format: \""+ep+"\"")
			return invalidParamsError
		}
		if len(parts[1]) < 1 {
			fmt.Fprintln(cmdCtx.err, "got empty endpoint from \""+ep+"\"")
			return invalidParamsError
		}

		ty, hasType := backend.TypeByName[parts[0]]
		if !hasType || ty == backend.UnknownBackend {
			fmt.Fprintln(cmdCtx.err, "unsupported endpoint type \""+parts[0]+"\"")
			return invalidParamsError
		}
		endpoints = append(endpoints, backend.Endpoint{
			Type:     ty,
			Endpoint: parts[1],
		})
	}

	net := a.mgr.GetNetwork(netName)
	if net == nil {
		fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" not found.")
		return invalidParamsError
	}

	router := net.Router()
	if router == nil || !net.Active() {
		fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" is down.")
		return invalidParamsError
	}
	if err := net.Router().SeedPeer(endpoints...); err != nil {
		fmt.Fprintf(cmdCtx.err, "failed to execute seed operation. (err = \"%v\")", err)
		return err
	}

	fmt.Fprintln(cmdCtx.out, "succeeded.")

	return nil
}

func (a *coreDaemonApplication) ReloadStaticConfig(path string) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if len(path) < 1 {
		a.log.Error("got a empty config path. ignore it.")
		return
	}

	a.configPath = path

	if a.mgr != nil {
		errs := a.mgr.UpdateConfigFromFile(a.configPath)
		if errs.AsError() != nil {
			a.log.Info("failed to update static config. (err = \"%v\")", errs)
		}
	}
}

func (a *coreDaemonApplication) Launch(arbiter *arbit.Arbiter, log *logging.Entry, ctx *cli.Context) (err error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.mgr != nil {
		panic("launch twice")
	}

	a.mgr = control.NewNetworkManager(arbiter, nil)
	if errs := a.mgr.UpdateConfigFromFile(a.configPath); errs.AsError() != nil {
		return errs.AsError()
	}
	// legacy.
	for _, netName := range ctx.Args().Slice() {
		net := a.mgr.GetNetwork(netName)
		if net == nil {
			a.log.Error("network \"%v\" not found.", netName)
			continue
		}
		if err = net.Up(); err != nil {
			a.log.Error("network setup failure: ", err)
			continue
		}
	}

	return nil
}

func (a *coreDaemonApplication) WantCommands() []*cli.Command {
	// TODO(xutao): cache []*cli.Command.
	app, _ := a.newCliApp(nil, nil)
	return app.Commands
}

const coreDaemonRunContextRawArgsKey = runContextKey("coreDaemonRunArgs")

type coreDaemonApplicationCommandContext struct {
	app      *coreDaemonApplication
	args     []string
	out, err io.Writer

	retry int
}

func (a *coreDaemonApplication) ExecuteCommand(out, err io.Writer, args []string) error {
	cli, cmdCtx := a.newCliApp(out, err)
	cmdCtx.args = args
	ctx := context.WithValue(context.Background(), coreDaemonRunContextRawArgsKey, cmdCtx)
	return cli.RunContext(ctx, args)
}
