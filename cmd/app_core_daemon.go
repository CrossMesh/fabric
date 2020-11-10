package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/edgerouter"
	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/fabric/metanet/backend"
	"github.com/crossmesh/fabric/metanet/backend/tcp"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
	"github.com/urfave/cli/v2"
	"go.etcd.io/bbolt"
	bolt "go.etcd.io/bbolt"
)

var (
	cmdErrInvalidParameters   = cmdError("invalid parameters")
	cmdErrResourceUnavaliable = cmdError("resource unavaliable")
)
var (
	metaNetStorePrefix        = []string{"metanet"}
	vnetControllerStorePrefix = []string{"vnet"}
)

type coreDaemonConfig struct {
	ConfigStore string `json:"config_store" yaml:"configStore" default:"/var/lib/crossmesh/config.db"`
}

type coreDaemonApplication struct {
	lock sync.RWMutex

	log *logging.Entry

	configPath string
	cfg        coreDaemonConfig

	manager    *edgerouter.EdgeRouter
	metaNet    *metanet.MetadataNetwork
	netDrivers struct {
		tcp *tcp.BackendManager
	}

	store     *bolt.DB
	storePath string

	arbiters struct {
		main    *arbit.Arbiter
		metanet *arbit.Arbiter
	}
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
			{
				Name:  "metanet",
				Usage: "configrate metadata network",
				Subcommands: []*cli.Command{
					{
						Name:  "tcp",
						Usage: "config tcp backend.",
						Subcommands: []*cli.Command{
							{
								Name:  "bind",
								Usage: "bind network endpoint.",
								Flags: []cli.Flag{
									&cli.BoolFlag{
										Name:        "permanent",
										Aliases:     []string{"p"},
										Usage:       "bind permanently.",
										Destination: &ctx.tcp.permanent,
										DefaultText: "false",
									},
								},
								ArgsUsage: "<bind_address>",
								Action:    a.cliTCPBindAction,
							},
							{
								Name:  "unbind",
								Usage: "unbind network endpoint.",
								Flags: []cli.Flag{
									&cli.BoolFlag{
										Name:        "permanent",
										Aliases:     []string{"p"},
										Usage:       "bind permanently.",
										Destination: &ctx.tcp.permanent,
										DefaultText: "false",
									},
								},
								ArgsUsage: "<bind_address>",
								Action:    a.cliTCPUnbindAction,
							},
							{
								Name:      "set-param",
								Usage:     "set network endpoint parameters.",
								ArgsUsage: "<bind_address> [arg_options...]",
								Action:    a.cliTCPSetParamAction,
							},
						},
					},
					{
						Name:      "set-endpoint-priority",
						Usage:     "set endpoint priority.",
						ArgsUsage: "<endpoint> <priority>",
						Action:    a.cliMetanetSetEndpointPriority,
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

	a.lock.RLock()
	if a.manager == nil {
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

		net := a.manager.GetNetwork(netName)
		if net == nil {
			fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" not found.")
			lastError = cmdErrInvalidParameters
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

	if ctx.Args().Len() < 1 {
		fmt.Fprintln(cmdCtx.err, "network missing.")
		return cmdErrInvalidParameters
	}
	if ctx.Args().Len() < 2 {
		fmt.Fprintln(cmdCtx.err, "missing active endpoint.")
		return cmdErrInvalidParameters
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	if a.manager == nil {
		return cmdError("network manager not started")
	}

	netName := ctx.Args().Get(0)

	var endpoints []backend.Endpoint

	for _, ep := range ctx.Args().Slice()[1:] {
		parts := strings.SplitN(ep, ":", 2)
		if len(parts) < 2 {
			fmt.Fprintln(cmdCtx.err, "endpoint with invalid format: \""+ep+"\"")
			return cmdErrInvalidParameters
		}
		if len(parts[1]) < 1 {
			fmt.Fprintln(cmdCtx.err, "got empty endpoint from \""+ep+"\"")
			return cmdErrInvalidParameters
		}

		ty, hasType := backend.TypeByName[parts[0]]
		if !hasType || ty == backend.UnknownBackend {
			fmt.Fprintln(cmdCtx.err, "unsupported endpoint type \""+parts[0]+"\"")
			return cmdErrInvalidParameters
		}
		endpoints = append(endpoints, backend.Endpoint{
			Type:     ty,
			Endpoint: parts[1],
		})
	}

	net := a.manager.GetNetwork(netName)
	if net == nil {
		fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" not found.")
		return cmdErrInvalidParameters
	}

	router := net.Router()
	if router == nil || !net.Active() {
		fmt.Fprintln(cmdCtx.err, "network \""+netName+"\" is down.")
		return cmdErrInvalidParameters
	}
	if err := net.Router().SeedPeer(endpoints...); err != nil {
		fmt.Fprintf(cmdCtx.err, "failed to execute seed operation. (err = \"%v\")", err)
		return err
	}

	fmt.Fprintln(cmdCtx.out, "succeeded.")

	return nil
}

func (a *coreDaemonApplication) cliTCPBindAction(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	numOfArgs := ctx.Args().Len()
	if numOfArgs < 1 {
		fmt.Fprintln(cmdCtx.err, "bind address is missing.")
		return cmdErrInvalidParameters
	}
	if numOfArgs > 1 {
		fmt.Fprintln(cmdCtx.err, "too many bind address.")
		return cmdErrInvalidParameters
	}

	ep := ctx.Args().Get(0)

	a.lock.RLock()
	defer a.lock.RUnlock()

	driver := a.netDrivers.tcp
	if driver == nil {
		fmt.Fprintln(cmdCtx.err, "TCP metanet driver is unavaliable.")
		return cmdErrResourceUnavaliable
	}
	if err := driver.Bind(ep, cmdCtx.tcp.permanent); err != nil {
		fmt.Fprintf(cmdCtx.err, "failed to bind %v. (err = \"%v\") \n", ep, err)
		return err
	}
	return nil
}

func (a *coreDaemonApplication) cliTCPUnbindAction(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	numOfArgs := ctx.Args().Len()
	if numOfArgs < 1 {
		fmt.Fprintln(cmdCtx.err, "bind address is missing.")
		return cmdErrInvalidParameters
	}
	if numOfArgs > 1 {
		fmt.Fprintln(cmdCtx.err, "too many bind address.")
		return cmdErrInvalidParameters
	}

	ep := ctx.Args().Get(0)

	a.lock.RLock()
	defer a.lock.RUnlock()
	driver := a.netDrivers.tcp
	if driver == nil {
		fmt.Fprintln(cmdCtx.err, "TCP metanet driver is unavaliable.")
		return cmdErrResourceUnavaliable
	}
	if err := driver.Unbind(ep, cmdCtx.tcp.permanent); err != nil {
		fmt.Fprintf(cmdCtx.err, "failed to unbind %v. (err = \"%v\") \n", ep, err)
		return err
	}
	return nil
}

func (a *coreDaemonApplication) cliTCPSetParamAction(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	numOfArgs := ctx.Args().Len()
	if numOfArgs < 1 {
		fmt.Fprintln(cmdCtx.err, "bind address is missing.")
		return cmdErrInvalidParameters
	}
	ep := ctx.Args().Get(0)

	a.lock.RLock()
	defer a.lock.RUnlock()
	driver := a.netDrivers.tcp
	if driver == nil {
		fmt.Fprintln(cmdCtx.err, "TCP metanet driver is unavaliable.")
		return cmdErrResourceUnavaliable
	}

	if err := driver.SetParams(ep, ctx.Args().Slice()[1:]); err != nil {
		fmt.Fprintln(cmdCtx.err, err)
		return err
	}

	return nil
}

func (a *coreDaemonApplication) cliMetanetSetEndpointPriority(ctx *cli.Context) error {
	cmdCtx := ctx.Context.Value(coreDaemonRunContextRawArgsKey).(*coreDaemonApplicationCommandContext)
	if cmdCtx == nil {
		return errors.New("nil command context")
	}

	numOfArgs := ctx.Args().Len()
	if numOfArgs < 1 {
		fmt.Fprintln(cmdCtx.err, "endpoint is missing.")
		return cmdErrInvalidParameters
	}
	if numOfArgs > 1 {
		fmt.Fprintln(cmdCtx.err, "cannot understand operation.")
		return cmdErrInvalidParameters
	}

	// parse endpoint
	ep := ctx.Args().Get(0)
	parts := strings.SplitN(ep, ":", 2)
	if len(parts) < 2 {
		fmt.Fprintln(cmdCtx.err, "endpoint with invalid format: \""+ep+"\"")
		return cmdErrInvalidParameters
	}
	if len(parts[1]) < 1 {
		fmt.Fprintln(cmdCtx.err, "got empty endpoint from \""+ep+"\"")
		return cmdErrInvalidParameters
	}
	ty, hasType := backend.TypeByName[parts[0]]
	if !hasType || ty == backend.UnknownBackend {
		fmt.Fprintln(cmdCtx.err, "unsupported endpoint type \""+parts[0]+"\"")
		return cmdErrInvalidParameters
	}
	endpoint := backend.Endpoint{Type: ty, Endpoint: ep}

	// parse priority.
	priorityValue := ctx.Args().Get(1)
	rpr, err := strconv.ParseUint(priorityValue, 10, 32)
	if err != nil {
		fmt.Fprintf(cmdCtx.err, "invalid priority value \"%v\". \n", priorityValue)
		return cmdErrInvalidParameters
	}
	priority := uint32(rpr)

	a.lock.RLock()
	defer a.lock.RUnlock()

	metanet := a.metaNet
	if metanet == nil {
		fmt.Fprintln(cmdCtx.err, "metanet is unavaliable.")
		return cmdErrResourceUnavaliable
	}
	if err = metanet.SetEndpointPriority(endpoint, priority); err != nil {
		fmt.Fprintln(cmdCtx.err, err)
		return err
	}
	return nil
}

func (a *coreDaemonApplication) ReloadStaticConfig(path string) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if len(path) < 1 {
		a.log.Error("got a empty config path. ignore it.")
		return
	}

	// core daemon config.
	a.configPath = path
	if err := common.LoadConfigFromFile(a.configPath, a.cfg); err != nil {
		logging.Error("failed to reload core daemon static config. (err = \"%v\")", err)
	}
	if a.store != nil && a.configPath != a.cfg.ConfigStore {
		// TODO(xutao): change network store in run time? it's really tricky.
		logging.Warn("store path changed, but store isn't supported to be reloaded in run time.")
	}

	if a.manager != nil {
		a.manager.ReloadStaticConfig(a.configPath)
	}
}

func (a *coreDaemonApplication) Launch(arbiter *arbit.Arbiter, log *logging.Entry, ctx *cli.Context) (err error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.manager != nil {
		panic("launch twice")
	}

	a.arbiters.main = arbiter
	a.arbiters.metanet = arbit.New()
	defer func() {
		if err != nil {
			a.arbiters.metanet.Shutdown()
		}
	}()

	if a.store == nil {
		// load config database.
		if a.cfg.ConfigStore == "" {
			err = errors.New("got empty config store path")
			logging.Error(err)
			return
		}
		a.store, err = bbolt.Open(a.cfg.ConfigStore, 0600, nil)
		if err != nil {
			logging.Error("failed to open configuration database. [path = \"%v\"] (err = \"%v\")", a.cfg.ConfigStore, err)
			return
		}
		a.configPath = a.cfg.ConfigStore
	}

	store := common.BoltDBStore(a.store)

	// metadata network.
	if a.metaNet, err = metanet.NewMetadataNetwork(
		a.arbiters.metanet,
		a.log.WithField("module", "metanet"),
		&common.SubpathStore{
			Store:  store,
			Prefix: metaNetStorePrefix,
		}); err != nil {
		return err
	}
	a.arbiters.main.Go(func() {
		<-a.arbiters.main.Exit()
		a.arbiters.metanet.Shutdown()
		a.arbiters.metanet.Join()
	})
	a.netDrivers.tcp = &tcp.BackendManager{}
	if err = a.metaNet.RegisterBackendManager(a.netDrivers.tcp); err != nil {
		logging.Error("failed to register tcp network backend (err = \"%v\")", err)
		return err
	}

	// vnet controller.
	if a.manager, err = edgerouter.New(
		a.arbiters.main,
		a.metaNet,
		a.log.WithField("module", "vnet_ctl"),
		&common.SubpathStore{
			Store:  store,
			Prefix: vnetControllerStorePrefix,
		},
	); err != nil {
		return err
	}

	a.manager.ReloadStaticConfig(a.configPath)

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

	tcp struct {
		permanent bool
	}

	retry int
}

func (a *coreDaemonApplication) ExecuteCommand(out, err io.Writer, args []string) error {
	cli, cmdCtx := a.newCliApp(out, err)
	cmdCtx.args = args
	ctx := context.WithValue(context.Background(), coreDaemonRunContextRawArgsKey, cmdCtx)
	return cli.RunContext(ctx, args)
}
