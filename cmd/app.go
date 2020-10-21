package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/crossmesh/fabric/cmd/pb"
	log "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
)

var (
	resultInvalidRequest = &pb.Result{Type: pb.Result_failed, Message: "invalid request"}
	resultOK             = &pb.Result{Type: pb.Result_succeeded, Message: "ok"}

	ErrInvalidDaemonCommand = errors.New("invalid daemon command")
)

func cmdError(format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	return err
}

type delegationApplication interface {
	AppName() string
}

type localCommandProvider interface {
	LocalCommands() []*cli.Command
}

type daemonApplication interface {
	delegationApplication

	Launch(arbiter *arbit.Arbiter, log *log.Entry, ctx *cli.Context) error
}

type commandAccepter interface {
	delegationApplication

	ExecuteCommand(out, err io.Writer, args []string) error
	WantCommands() []*cli.Command
}

//type commandCompleter interface {
//	CompleteCommand(string) []string
//}

type staticConfigurationAccpeter interface {
	ReloadStaticConfig(path string)
}

// ControlRPC contains configuration of control port.
type controlRPCConfig struct {
	Type     string `json:"type" yaml:"type" default:"unix"`
	Endpoint string `json:"endpoint" yaml:"endpoint" default:"/var/run/utt_control.sock"`
}

type daemonConfig struct {
	Control *controlRPCConfig `json:"control" yaml:"control" default:"{}"`
	Debug   *bool             `json:"debug" yaml:"debug"`
}

func (c *controlRPCConfig) Equal(x *controlRPCConfig) bool { return reflect.DeepEqual(c, x) }

// CrossmeshApplication implements crossmesh application frame.
type CrossmeshApplication struct {
	lock sync.RWMutex

	rawArgs []string

	cli *cli.App
	log *log.Entry

	arbiters struct {
		main    *arbit.Arbiter
		control *arbit.Arbiter
		apps    *arbit.Arbiter
	}

	apps []delegationApplication

	daemonCmd *cli.Command
	edgeCmd   *cli.Command // deprecated 'edge' command.

	ConfigFile string
	config     *daemonConfig

	// control RPC.
	control struct {
		config    *controlRPCConfig
		listener  net.Listener
		rpcServer *grpc.Server
	}
	pb.UnimplementedDaemonControlServer // make gRPC happy.

	Retry int
}

// NewApp create crossmesh application instance.
func NewApp() (a *CrossmeshApplication) {
	a = &CrossmeshApplication{
		log: log.NewEntry(log.StandardLogger()),
	}

	// shim app. only for flags early parsing.
	a.cli = &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "config file",
				Destination: &a.ConfigFile,
				DefaultText: "/etc/utt.yml",
			},
		},
		HideHelp: true,
		Action:   a.cliRunShimAction,
	}
	for _, app := range getDelegationApplications(a, a.log) {
		if app == nil {
			continue
		}
		a.apps = append(a.apps, app)
	}

	return
}

// Run runs application.
func (a *CrossmeshApplication) Run(args []string) error {
	// inject raw args to context. so the command action could inspect related raw arguments.
	ctx := context.WithValue(context.Background(), runContextRawArgsKey, &crossmeshApplicationRunContext{
		app:  a,
		args: args,
	})
	return a.cli.RunContext(ctx, args)
}

func createControlClient(cfg *controlRPCConfig) (conn *grpc.ClientConn, err error) {
	if cfg == nil {
		return nil, cmdError("empty control RPC configuration. cannot connect to daemon.")
	}
	if conn, err = grpc.Dial(cfg.Endpoint, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(cfg.Type, addr, timeout)
	}), grpc.WithInsecure()); err != nil {
		log.Error("connect to control RPC failure: ", err)
		return nil, err
	}
	return
}

// GetControlRPCClient creates RPC DaemonControlClient.
func (a *CrossmeshApplication) GetControlRPCClient() (conn *grpc.ClientConn, client pb.DaemonControlClient, err error) {
	a.lock.Lock()
	cfg := a.getStaticDaemonConfig(true)
	a.lock.Unlock()

	if cfg == nil {
		return nil, nil, errors.New("cannot load configration")
	}

	if conn, err = createControlClient(cfg.Control); err != nil {
		return conn, client, err
	}

	return conn, pb.NewDaemonControlClient(conn), nil
}
