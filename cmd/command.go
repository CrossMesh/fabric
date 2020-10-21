package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/crossmesh/fabric/cmd/pb"
	"github.com/crossmesh/fabric/cmd/version"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type hashTrieNode struct {
	name   string
	value  interface{}
	childs map[string]*hashTrieNode
}

type hashTrie map[string]*hashTrieNode

func (t *hashTrie) searchForNode(path []string, create bool) (parent, node *hashTrieNode) {
	if len(path) < 1 {
		return nil, nil
	}
	if *t == nil {
		if !create {
			return nil, nil
		}
		*t = make(hashTrie)
	}
	if node, _ = (*t)[path[0]]; node == nil {
		if !create {
			return nil, nil
		}
		node = &hashTrieNode{name: path[0]}
		(*t)[path[0]] = node
	}
	// down
	for i := 1; i < len(path); i++ {
		key := path[0]
		var child *hashTrieNode
		if node.childs != nil {
			child, _ = node.childs[key]
		} else {
			if !create {
				return nil, nil
			}
			node.childs = make(map[string]*hashTrieNode)
		}
		if child == nil {
			if !create {
				return nil, nil
			}
			child = &hashTrieNode{name: path[0]}
			node.childs[key] = child
		}
		parent = node
		node = child
	}
	return
}

// define internal commands of whole application.
func (a *CrossmeshApplication) internalCommands() []*cli.Command {
	a.daemonCmd = &cli.Command{
		Name:   "daemon",
		Usage:  "run as daemon.",
		Action: a.cliRunDaemonAction,

		Subcommands: []*cli.Command{
			{
				Name:   "reload",
				Usage:  "reload config.",
				Action: a.cliReloadCmdAction,
			},
		},
	}
	a.edgeCmd = &cli.Command{
		Name:   "edge",
		Hidden: true,
		Action: func(ctx *cli.Context) error {
			a.log.Warn("\"edge\" command is deprecated. please use \"daemon\" command instead.")
			return a.cliRunDaemonAction(ctx)
		},
		Subcommands: []*cli.Command{
			{
				Name: "reload",
				Action: func(ctx *cli.Context) error {
					a.log.Warn("\"edge\" command is deprecated. please use \"daemon\" command instead.")
					return a.cliReloadCmdAction(ctx)
				},
			},
		},
	}

	return []*cli.Command{a.daemonCmd, a.edgeCmd}
}

type commandExecutionOutputForwarder struct {
	log         *log.Entry
	req         *pb.CommandExecuteRequest
	stream      pb.DaemonControl_ExecuteCommandServer
	forwardType pb.CommandExecuteResult_Type
}

func (f *commandExecutionOutputForwarder) Write(payload []byte) (int, error) {
	if err := f.stream.Send(&pb.CommandExecuteResult{
		Id:      f.req.Id,
		Type:    f.forwardType,
		Payload: string(payload),
	}); err != nil {
		f.log.Errorf("error occurs when trying to forward output. (err = \"%v\")", err)
		return 0, err
	}
	return len(payload), nil
}

func prepareDaemonCommand(args []string) (actual []string, prefixes []string) {
	if len(args) < 2 {
		return args, nil
	}

	// extract command prefixes.
	i := 1
	for started := false; i < len(args); i++ {
		arg := args[i]
		if len(arg) > 0 && arg[0] == '-' {
			if started { // command part ended.
				break
			}
			// got a global option. skip.
			i++
			continue
		} else {
			started = true
		}
		prefixes = append(prefixes, arg)
	}

	// build the actual.
	actual = make([]string, 0, 1+len(prefixes)+len(args[i:]))
	actual = append(actual, args[0])
	actual = append(actual, prefixes...)
	actual = append(actual, args[i:]...)

	return
}

func traverseCliSubcommandTree(commands []*cli.Command, visit func([]*cli.Command) bool) {
	type stackEntry struct {
		*cli.Command
		childIdx int
	}

	stacks := make([]*stackEntry, 0, len(commands))
	commandStack := []*cli.Command{}
	for _, command := range commands {
		if command == nil {
			if !visit(commandStack) {
				return
			}
			continue
		}
		stacks = append(stacks, &stackEntry{
			Command:  command,
			childIdx: 0,
		})
		commandStack = append(commandStack, command)

		for len(stacks) > 0 {
			top := len(stacks) - 1
			entry := stacks[top]
			if entry.childIdx < len(entry.Command.Subcommands) {
				child := entry.Command.Subcommands[entry.childIdx]
				stacks = append(stacks, &stackEntry{
					Command:  child,
					childIdx: 0,
				})
				commandStack = append(commandStack, child)
				entry.childIdx++
			} else {
				if !visit(commandStack) {
					return
				}
				stacks = stacks[:top]
				commandStack = commandStack[:top]
			}
		}
	}
}

// GetDaemonCommands implements gRPC method "GetDaemonCommands".
func (a *CrossmeshApplication) GetDaemonCommands(req *pb.DaemonCommandListRequest, stream pb.DaemonControl_GetDaemonCommandsServer) (err error) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	numOfCommands := 0
	for _, app := range a.apps {
		accepter, isAccepter := app.(commandAccepter)
		if !isAccepter {
			continue
		}

		// ignore conficts now?
		path := []string{}
		traverseCliSubcommandTree(accepter.WantCommands(), func(stack []*cli.Command) bool {
			if len(stack) < 1 {
				return true
			}
			path = path[:0]
			for i := 0; i < len(stack); i++ {
				cmd := stack[i]
				path = append(path, cmd.Name)
			}
			cmd := stack[len(stack)-1]
			if err = stream.Send(&pb.DaemonCommand{
				Name:     path,
				Aliases:  cmd.Aliases,
				Category: cmd.Category,
				Usage:    cmd.Usage,
			}); err != nil {
				a.log.Errorf("error occurs when sending daemon commands. (err = \"%v\")", err)
				return false
			}
			numOfCommands++
			return true
		})
		if err != nil {
			return err
		}
	}

	a.log.Debugf("client trys to get daemon commands. %v commands offered.", numOfCommands)

	return nil
}

// ExecuteCommand implements gRPC method "ExecuteCommand".
func (a *CrossmeshApplication) ExecuteCommand(stream pb.DaemonControl_ExecuteCommandServer) (err error) {
	type commandWant struct {
		prefixes []string
		app      commandAccepter
		actual   []string
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	forwarderLog := a.log.WithField("module", "execution_output_forwarder")

	for {
		req, ierr := stream.Recv()
		if ierr != nil {
			if ierr == io.EOF {
				ierr = nil
			}
			if status, isStatus := status.FromError(ierr); isStatus {
				if status.Code() == codes.Canceled {
					ierr = nil
				}
			}
			if ierr != nil {
				err = ierr
				a.log.Errorf("error occurs when receiving command. (err = \"%v\")", err)
			}
			break
		}
		if len(req.Args) < 2 {
			if err = stream.Send(&pb.CommandExecuteResult{
				Type:    pb.CommandExecuteResult_failed,
				Payload: ErrInvalidDaemonCommand.Error(),
				Id:      req.Id,
			}); err != nil {
				a.log.Errorf("failed to send invalid daemon command error to client. (err = \"%v\")", err)
				return err
			}
			continue
		}

		a.log.Debugf("client send daemon command = \"%v\"", strings.Join(req.Args, " "))

		actualCommand, commandPrefixes := prepareDaemonCommand(req.Args)

		// start execute command.
		// find runner. match command wants.
		var possibleWant *commandWant

		// TODO(xutao): using a pre-built trie may be faster. trie can be also used to implements command completion.
		for _, app := range a.apps {
			accepter, isAccepter := app.(commandAccepter)
			if !isAccepter {
				continue
			}

			commands := accepter.WantCommands()
			traverseCliSubcommandTree(commands, func(path []*cli.Command) bool {
				prefixes := make([]string, 0, len(path))
				for i := 0; i < len(path); i++ {
					prefixes = append(prefixes, path[i].Name)
				}

				if len(prefixes) < 1 {
					// we ignore empty prefix for now, respecting following considerations:
					//   1. If a empty prefix match all commands, it will be easy to bring ambiguous matches.
					//   2. Or we treat it as a special case that it only matches blank command. This case is meaningless
					//		because blank command is equivalent to `--help` without any arguments and flags.
					//		So it shouldn't be forward to daemon.
					return true
				}

				var prevPrefixes []string
				if possibleWant != nil {
					prevPrefixes = possibleWant.prefixes
				}
				if len(prefixes) < len(prevPrefixes) { // longest prefix first.
					return true
				}
				matchIdx := 0
				for ; matchIdx < len(commandPrefixes) && matchIdx < len(prefixes); matchIdx++ {
					if commandPrefixes[matchIdx] != prefixes[matchIdx] {
						break
					}
				}
				if matchIdx < len(prefixes) { // unmatched.
					return true
				}
				if possibleWant != nil {
					if possibleWant.app != accepter { // ambiguous matches.
						if err = stream.Send(&pb.CommandExecuteResult{
							Type: pb.CommandExecuteResult_failed,
							Payload: fmt.Sprintf("ambiguous command: %v. App \"%v\" wants \"%v\". And app \"%v\" wants \"%v\"",
								req.Args,
								possibleWant.app.AppName(), strings.Join(possibleWant.prefixes, " "),
								app.AppName(), strings.Join(prefixes, " "),
							),
							Id: req.Id,
						}); err != nil {
							a.log.Errorf("failed to send \"ambiguous command\" error to client. (err = \"%v\")", err)
							return false
						}
					}
					possibleWant.prefixes = prefixes
					possibleWant.actual = actualCommand
				} else {
					possibleWant = &commandWant{
						prefixes: prefixes,
						app:      accepter,
						actual:   actualCommand,
					}
				}

				return true
			})

			if err != nil {
				return err
			}
		}

		if possibleWant == nil {
			if err = stream.Send(&pb.CommandExecuteResult{
				Type:    pb.CommandExecuteResult_failed,
				Payload: "no suitable application found for the command.",
				Id:      req.Id,
			}); err != nil {
				a.log.Errorf("failed to send \"command not found\" error to client. (err = \"%v\")", err)
				return err
			}
			continue
		}

		outForwarder, errForwarder := &commandExecutionOutputForwarder{
			stream:      stream,
			req:         req,
			forwardType: pb.CommandExecuteResult_stdout,
			log:         forwarderLog,
		}, &commandExecutionOutputForwarder{
			stream:      stream,
			req:         req,
			forwardType: pb.CommandExecuteResult_stderr,
			log:         forwarderLog,
		}

		if err := possibleWant.app.ExecuteCommand(outForwarder, errForwarder, possibleWant.actual); err != nil {
			if err = stream.Send(&pb.CommandExecuteResult{
				Type:    pb.CommandExecuteResult_failed,
				Payload: "execution failure = \"" + err.Error() + "\"",
				Id:      req.Id,
			}); err != nil {
				a.log.Errorf("failed to send \"execution failure\" error to client. (err = \"%v\")", err)
				return err
			}
		}
		if err = stream.Send(&pb.CommandExecuteResult{
			Type:    pb.CommandExecuteResult_succeeded,
			Payload: "ok",
			Id:      req.Id,
		}); err != nil {
			a.log.Errorf("failed to send \"succeeded\" message to client. (err = \"%v\")", err)
			return err
		}
	}

	return err
}

type runContextKey string

const runContextRawArgsKey = runContextKey("rawArgs")

type crossmeshApplicationRunContext struct {
	app      *CrossmeshApplication
	args     []string
	warnMsgs []string
}

// ExecuteCommandOnDaemon forwards and executes command on daemon.
func (a *CrossmeshApplication) ExecuteCommandOnDaemon(args []string) (bool, error) {
	if len(args) < 2 {
		return false, ErrInvalidDaemonCommand
	}

	conn, client, err := a.GetControlRPCClient()
	if err != nil {
		a.log.Errorf("failed to get control RPC client. (err = \"%v\")", err)
		return false, err
	}
	defer conn.Close()

	// forward command to daemon.
	req := pb.CommandExecuteRequest{
		Args: args,
	}
	var executor pb.DaemonControl_ExecuteCommandClient
	if executor, err = client.ExecuteCommand(context.TODO()); err != nil {
		a.log.Errorf("failed to create ExecuteCommand RPC stream. (err = \"%v\")", err)
		return false, err
	}
	if err = executor.Send(&req); err != nil {
		a.log.Errorf("RPC failed to send command. (err = \"%v\")", err)
		return false, err
	}

	var finalResult *pb.CommandExecuteResult
receiveCommandOutput:
	for {
		result, err := executor.Recv()
		if err != nil {
			if err == io.EOF {
				return false, err
			}
			if err != nil {
				a.log.Errorf("RPC failed to receive results. (err = \"%v\")", err)
			}
			break
		}
		switch result.Type {
		case pb.CommandExecuteResult_failed, pb.CommandExecuteResult_succeeded:
			finalResult = result
			break receiveCommandOutput

		case pb.CommandExecuteResult_stdout:
			os.Stdout.Write([]byte(result.Payload))

		case pb.CommandExecuteResult_stderr:
			os.Stderr.Write([]byte(result.Payload))

		default:
			a.log.Warnf("receive CommandExecuteResult with unknown type %v", result.Type)
		}
	}
	if finalResult == nil {
		return false, cmdError("RPC command execution terminated unexpectedly")
	} else if finalResult.Type == pb.CommandExecuteResult_succeeded {
		fmt.Println("command succeeded:", finalResult.Payload)
	} else if finalResult.Type == pb.CommandExecuteResult_failed {
		fmt.Println("command failed:", finalResult.Payload)
	}

	return finalResult.Type == pb.CommandExecuteResult_succeeded, nil
}

func (a *CrossmeshApplication) newCompletedCliApp() (app *cli.App, warnMessage []string, err error) {
	commandTrie := make(hashTrie)

	type commandContext struct {
		daemon, ignored bool
		needParent      bool
		*cli.Command
		owner delegationApplication
	}

	// TODO(xutao): use typed error.
	buildCommandConflictError := func(path []string, oa, ob delegationApplication) error {
		if oa == nil {
			ob, oa = oa, ob
		}
		cmdString := strings.Join(path, " ")
		if oa == ob {
			if oa == nil {
				return fmt.Errorf("more then one internal command \"%v\" found", cmdString)
			}
			return fmt.Errorf("more then one command \"%v\" found for app \"%v\"", cmdString, oa.AppName())
		}
		if ob == nil {
			return fmt.Errorf("command \"%v\" of app \"%v\" conflicts with internal command", cmdString, oa.AppName())
		}
		return fmt.Errorf("app \"%v\" and app \"%v\" conflict at local command \"%v\"", oa.AppName(), ob.AppName(), cmdString)
	}

	insertLocalCommands := func(commands []*cli.Command, owner delegationApplication) (err error) {
		traverseCliSubcommandTree(commands, func(stack []*cli.Command) bool {
			path := make([]string, 0, len(stack))
			for i := 0; i < len(stack); i++ {
				cmd := stack[i]
				path = append(path, cmd.Name)

				_, node := commandTrie.searchForNode(path, true)
				if node.value != nil {
					// check conflicts.
					ctx := node.value.(*commandContext)
					if ctx.owner != nil || ctx.Command != stack[i] {
						err = buildCommandConflictError(path, ctx.owner, nil)
						return false
					}
					return true
				}
				node.value = &commandContext{
					daemon:  false,
					Command: stack[i],
					owner:   nil,
					ignored: false,
				}
			}
			return true
		})
		return err
	}

	// internal commands.
	if err = insertLocalCommands(a.internalCommands(), nil); err != nil {
		return nil, nil, err
	}

	a.lock.RLock()
	// local commands.
	for _, app := range a.apps {
		commandProvider, isCommandProvider := app.(localCommandProvider)
		if !isCommandProvider {
			continue
		}

		if err := insertLocalCommands(commandProvider.LocalCommands(), app); err != nil {
			return nil, nil, nil
		}
	}
	a.lock.RUnlock()

	// consult daemon for more commands.
	conn, client, connErr := a.GetControlRPCClient()
	var transmitError error
	for connErr == nil {
		defer conn.Close()

		rpcCtx, cancelRPC := context.WithTimeout(context.TODO(), time.Second*30)
		defer cancelRPC()
		stream, err := client.GetDaemonCommands(rpcCtx, &pb.DaemonCommandListRequest{})
		if err != nil {
			connErr = err
			break
		}

		for {
			var resp *pb.DaemonCommand

			if resp, err = stream.Recv(); err != nil {
				if err == io.EOF {
					err = nil
				} else if status, isStatus := status.FromError(err); isStatus && status.Code() == codes.Canceled {
					err = nil
				}
				if err != nil {
					transmitError = err
				}
				break
			}
			if resp == nil {
				continue
			}

			if len(resp.Name) > 1 {
				// only the first subcommand will be handle locally.
				continue
			}

			_, node := commandTrie.searchForNode(resp.Name, true)
			if node.value != nil {
				// check conflicts.
				ctx := node.value.(*commandContext)
				warn := ""
				if ctx.daemon {
					// daemon command conflicts.
					ctx.ignored = true
					warn = fmt.Sprintf("daemon command \"%v\" ignored due to conflict.", strings.Join(resp.Name, " "))
				} else {
					// local command overwrites daemon command.
					warn = fmt.Sprintf("daemon command \"%v\" ignored due to conflict with the local one.", strings.Join(resp.Name, " "))
				}
				warnMessage = append(warnMessage, warn)

			} else {
				node.value = &commandContext{
					daemon:  true,
					ignored: false,
					Command: &cli.Command{
						Name:     resp.Name[len(resp.Name)-1],
						Usage:    resp.Usage,
						Category: resp.Category,
						Aliases:  resp.Aliases,
						Action:   a.cliRunDefaultAction,
					},
					needParent: true,
				}
			}
		}
		break
	}
	enableDaemonCommand := true
	if connErr != nil {
		warn := fmt.Sprintf("cannot talk to daemon. more commands won't be avaliable until daemon connected. (err = \"%v\")", connErr)
		warnMessage = append(warnMessage, warn)
	} else if transmitError != nil {
		warn := fmt.Sprintf("daemon commands are not avaliable due to unexpected termination of transport. retry may help. (err = \"%v\")", transmitError)
		warnMessage = append(warnMessage, warn)
		enableDaemonCommand = false
	}

	// build actual cli command tree.
	validCommandNode := func(ctx *commandContext) bool {
		if ctx.Command == nil {
			// TODO(xutao): log invalid child commmands.
			return false
		} else if ctx.daemon {
			if !enableDaemonCommand || ctx.ignored {
				return false
			}
		}
		return true
	}
	for rootName, root := range commandTrie {
		layer, nextLayer := []*hashTrieNode{}, []*hashTrieNode{}
		cmdCtx := root.value.(*commandContext)
		if !validCommandNode(cmdCtx) {
			delete(commandTrie, rootName)
			continue
		}
		layer = append(layer, root)
		for _, node := range layer {
			parentCmdCtx := node.value.(*commandContext)
			for _, node := range node.childs {
				cmdCtx := node.value.(*commandContext)
				if !validCommandNode(cmdCtx) {
					continue // cut.
				}
				if cmdCtx.needParent {
					parentCmdCtx.Command.Subcommands = append(parentCmdCtx.Command.Subcommands, cmdCtx.Command)
				}
				nextLayer = append(nextLayer, node)
			}
		}
		if len(nextLayer) < 1 {
			break
		}
		layer, nextLayer = nextLayer, layer[:0]
	}

	// build app.
	app = &cli.App{
		Name:                 "crossmesh",
		Usage:                "Overlay network router, designed for connecting cloud network infrastructure",
		Version:              version.Version + "+rev" + version.Revision,
		EnableBashCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "config file",
				Destination: &a.ConfigFile,
				DefaultText: "/etc/utt.yml",
			},
		},
		Action: a.cliRunDefaultAction,
	}
	for _, root := range commandTrie {
		cmdCtx := root.value.(*commandContext)
		app.Commands = append(app.Commands, cmdCtx.Command)
	}

	return
}

func (a *CrossmeshApplication) cliRunShimAction(ctx *cli.Context) (err error) {
	runCtx := ctx.Context.Value(runContextRawArgsKey).(*crossmeshApplicationRunContext)
	app, warnMessage, err := a.newCompletedCliApp()
	if err != nil {
		a.log.Errorf("cannot build real cli app. (err = \"%v\")", err)
		return err
	}
	runCtx.warnMsgs = warnMessage
	newCtx := context.WithValue(context.Background(), runContextRawArgsKey, runCtx)
	return app.RunContext(newCtx, runCtx.args)
}

func (a *CrossmeshApplication) cliRunDefaultAction(ctx *cli.Context) (err error) {
	runCtx := ctx.Context.Value(runContextRawArgsKey).(*crossmeshApplicationRunContext)
	actual, _ := prepareDaemonCommand(runCtx.args)
	if len(actual) < 2 {
		cli.ShowAppHelp(ctx)
		for _, msg := range runCtx.warnMsgs {
			a.log.Warn(msg)
		}
		return
	}

	succeeded, err := a.ExecuteCommandOnDaemon(actual)
	if err != nil {
		return err
	}
	if !succeeded {
		return cmdError("command execution failed")
	}
	return nil
}
