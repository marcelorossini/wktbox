package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"wktbox/internal/agentintegration"
	"wktbox/internal/app"
	"wktbox/internal/discovery"
	"wktbox/internal/executor"
	"wktbox/internal/loopback"
	"wktbox/internal/output"
	"wktbox/internal/portforward"
	"wktbox/internal/prune"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
	"wktbox/internal/version"
)

var ErrForceRequired = errors.New("destroy requires confirmation or --force")

type ExitError struct {
	Code int
}

func (err ExitError) Error() string {
	return fmt.Sprintf("child command exited with code %d", err.Code)
}

type Streams struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	Terminal   bool
	WorkingDir string
}

type Service interface {
	Resolve(context.Context, app.Request) (app.Resolution, error)
	Ensure(context.Context, app.Resolution) (state.BoxRecord, error)
	Inspect(context.Context, app.Resolution) (state.BoxRecord, error)
	List(context.Context) ([]state.BoxRecord, error)
	Connect(context.Context, []string, string) (state.ConnectionRecord, error)
	Connections(context.Context, string) ([]state.ConnectionRecord, error)
	Disconnect(context.Context, string) (state.ConnectionRecord, error)
	Prune(context.Context, bool) (prune.Report, error)
	Run(context.Context, state.BoxRecord, []string, executor.Options) (int, error)
	SyncLoopback(context.Context, state.BoxRecord) (loopback.Status, error)
	Logs(context.Context, state.BoxRecord, string, bool, io.Writer, io.Writer) (int, error)
	Stop(context.Context, app.Resolution) error
	Restart(context.Context, app.Resolution) error
	Destroy(context.Context, app.Resolution) error
	Open(context.Context, state.BoxRecord) error
	Doctor(context.Context, app.Request) (any, error)
	ImportPorts(context.Context, state.BoxRecord, []portforward.Mapping) ([]portforward.ObservedMapping, error)
	PublishPorts(context.Context, state.BoxRecord, []portforward.Mapping) ([]portforward.ObservedMapping, error)
	PortMappings(context.Context, state.BoxRecord) ([]portforward.ObservedMapping, error)
	RemovePortMappings(context.Context, state.BoxRecord, []string, portforward.Direction) ([]portforward.ObservedMapping, error)
}

type AgentManager interface {
	Install(
		context.Context,
		agentintegration.Options,
	) (agentintegration.Report, error)
	Status(
		context.Context,
		agentintegration.Options,
	) (agentintegration.Report, error)
	Uninstall(
		context.Context,
		agentintegration.Options,
	) (agentintegration.Report, error)
}

type Dependencies struct {
	Service Service
	Agents  AgentManager
}

type flags struct {
	path        string
	configPath  string
	envFile     string
	envTarget   string
	profile     string
	json        bool
	quiet       bool
	verbose     bool
	noColor     bool
	environment []string
}

type commandSet struct {
	service Service
	agents  AgentManager
	streams Streams
	flags   *flags
}

func New(dependencies Dependencies, streams Streams) *cobra.Command {
	if streams.In == nil {
		streams.In = strings.NewReader("")
	}
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.Err == nil {
		streams.Err = io.Discard
	}
	options := &flags{}
	commands := commandSet{
		service: dependencies.Service,
		agents:  dependencies.Agents,
		streams: streams,
		flags:   options,
	}
	root := &cobra.Command{
		Use:           "wktbox",
		Short:         "Isolated development boxes for Git worktrees",
		Version:       version.String(),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)

	persistent := root.PersistentFlags()
	persistent.StringVar(&options.path, "path", ".", "Git worktree path")
	persistent.StringVar(&options.configPath, "config", "", "alternative configuration file")
	persistent.StringVar(&options.envFile, "env-file", "", "project environment file")
	persistent.StringVar(&options.envTarget, "env-target", "", "project environment mount target")
	persistent.StringVar(&options.profile, "profile", "", "configuration profile")
	persistent.BoolVar(&options.json, "json", false, "emit structured JSON")
	persistent.BoolVarP(&options.quiet, "quiet", "q", false, "reduce output")
	persistent.BoolVarP(&options.verbose, "verbose", "v", false, "emit verbose diagnostics")
	persistent.BoolVar(&options.noColor, "no-color", false, "disable color output")
	persistent.StringArrayVar(
		&options.environment,
		"env",
		nil,
		"set child command environment KEY=VALUE (repeatable)",
	)

	root.AddCommand(
		commands.up(),
		commands.run(),
		commands.exec(),
		commands.compose(),
		commands.shell(),
		commands.open(),
		commands.list(),
		commands.status(),
		commands.connect(),
		commands.connections(),
		commands.disconnect(),
		commands.logs(),
		commands.stop(),
		commands.restart(),
		commands.destroy(),
		commands.doctor(),
		commands.prune(),
		commands.portCommands(),
		commands.agentCommands(),
	)
	return root
}

func (commands commandSet) portCommands() *cobra.Command {
	command := &cobra.Command{
		Use:   "port",
		Short: "Import host services or publish box ports",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		commands.portImport(),
		commands.portPublish(),
		commands.portList(),
		commands.portRemove(),
	)
	return command
}

func (commands commandSet) portImport() *cobra.Command {
	var mapped []string
	command := &cobra.Command{
		Use:   "import <host-port> [<host-port>...]",
		Short: "Expose real-host TCP services as workload localhost ports",
		RunE: func(command *cobra.Command, arguments []string) error {
			mappings, err := portforward.ParseImports(arguments, mapped)
			if err != nil {
				return err
			}
			box, err := commands.portReadyBox(command.Context())
			if err != nil {
				return err
			}
			observed, err := commands.service.ImportPorts(
				command.Context(),
				box,
				mappings,
			)
			if err != nil {
				return err
			}
			return commands.renderer().PortMappings(observed)
		},
	}
	command.Flags().StringArrayVar(
		&mapped,
		"map",
		nil,
		"mapping name=host-address:host-port:container-port (repeatable)",
	)
	return command
}

func (commands commandSet) portPublish() *cobra.Command {
	var mapped []string
	command := &cobra.Command{
		Use:   "publish",
		Short: "Publish DinD TCP ports on real-host loopback",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			mappings, err := portforward.ParsePublications(mapped)
			if err != nil {
				return err
			}
			box, err := commands.portReadyBox(command.Context())
			if err != nil {
				return err
			}
			observed, err := commands.service.PublishPorts(
				command.Context(),
				box,
				mappings,
			)
			if err != nil {
				return err
			}
			return commands.renderer().PortMappings(observed)
		},
	}
	command.Flags().StringArrayVar(
		&mapped,
		"map",
		nil,
		"mapping name=bind-address:host-port:box-port (repeatable)",
	)
	return command
}

func (commands commandSet) portList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List desired and observed port mappings",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			box, err := commands.portReadyBox(command.Context())
			if err != nil {
				return err
			}
			mappings, err := commands.service.PortMappings(
				command.Context(),
				box,
			)
			if err != nil {
				return err
			}
			return commands.renderer().PortMappings(mappings)
		},
	}
}

func (commands commandSet) portRemove() *cobra.Command {
	var allImports bool
	var allPublications bool
	command := &cobra.Command{
		Use:   "remove <name> [<name>...]",
		Short: "Remove named mappings or every mapping in one direction",
		RunE: func(command *cobra.Command, arguments []string) error {
			allCount := 0
			if allImports {
				allCount++
			}
			if allPublications {
				allCount++
			}
			if len(arguments) != 0 && allCount != 0 {
				return errors.New(
					"use mapping names or one --all-* flag, not both",
				)
			}
			if len(arguments) == 0 && allCount != 1 {
				return errors.New(
					"provide mapping names, --all-imports, or --all-publications",
				)
			}
			direction := portforward.Direction("")
			if allImports {
				direction = portforward.Import
			}
			if allPublications {
				direction = portforward.Publish
			}
			box, err := commands.portReadyBox(command.Context())
			if err != nil {
				return err
			}
			mappings, err := commands.service.RemovePortMappings(
				command.Context(),
				box,
				arguments,
				direction,
			)
			if err != nil {
				return err
			}
			return commands.renderer().PortMappings(mappings)
		},
	}
	command.Flags().BoolVar(
		&allImports,
		"all-imports",
		false,
		"remove every host-to-workload import",
	)
	command.Flags().BoolVar(
		&allPublications,
		"all-publications",
		false,
		"remove every box-to-host publication",
	)
	return command
}

func (commands commandSet) portReadyBox(
	ctx context.Context,
) (state.BoxRecord, error) {
	if commands.service == nil {
		return state.BoxRecord{}, errors.New("sandbox service is not configured")
	}
	resolution, err := commands.resolve(ctx)
	if err != nil {
		return state.BoxRecord{}, err
	}
	box, err := commands.service.Inspect(ctx, resolution)
	if err != nil {
		return state.BoxRecord{}, err
	}
	if box.Status != state.Ready {
		return state.BoxRecord{}, boxNotReadyError(box.Status)
	}
	return box, nil
}

func (commands commandSet) connect() *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "connect <box> <box> [<box>...]",
		Short: "Connect boxes through their published workload ports",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(command *cobra.Command, arguments []string) error {
			if commands.service == nil {
				return errors.New("sandbox service is not configured")
			}
			record, err := commands.service.Connect(
				command.Context(),
				arguments,
				name,
			)
			if err != nil {
				return err
			}
			boxes, err := commands.service.List(command.Context())
			if err != nil {
				return err
			}
			return commands.renderer().Connection(record, boxRecordsByID(boxes))
		},
	}
	command.Flags().StringVar(
		&name,
		"name",
		"",
		"optional lowercase connection name",
	)
	return command
}

func (commands commandSet) connections() *cobra.Command {
	return &cobra.Command{
		Use:   "connections [connection]",
		Short: "Show cross-box connection topology",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if commands.service == nil {
				return errors.New("sandbox service is not configured")
			}
			selector := ""
			if len(arguments) == 1 {
				selector = arguments[0]
			}
			records, err := commands.service.Connections(
				command.Context(),
				selector,
			)
			if err != nil {
				return err
			}
			boxes, err := commands.service.List(command.Context())
			if err != nil {
				return err
			}
			recordsByID := boxRecordsByID(boxes)
			if selector != "" && len(records) == 1 {
				return commands.renderer().Connection(records[0], recordsByID)
			}
			return commands.renderer().Connections(records, recordsByID)
		},
	}
}

func (commands commandSet) disconnect() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <connection>",
		Short: "Remove one cross-box connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if commands.service == nil {
				return errors.New("sandbox service is not configured")
			}
			record, err := commands.service.Disconnect(
				command.Context(),
				arguments[0],
			)
			if err != nil {
				return err
			}
			name := record.Name
			if name == "" {
				name = record.ID
			}
			return commands.renderer().Message(
				"Connection " + name + " disconnected.",
			)
		},
	}
}

func (commands commandSet) prune() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "prune",
		Short: "Find or destroy boxes for worktrees that no longer exist",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if commands.service == nil {
				return errors.New("sandbox service is not configured")
			}
			report, err := commands.service.Prune(command.Context(), force)
			if err != nil {
				return err
			}
			return commands.renderer().PruneReport(report, force)
		},
	}
	command.Flags().BoolVar(
		&force,
		"force",
		false,
		"destroy boxes whose recorded worktrees no longer exist",
	)
	return command
}

func (commands commandSet) agentCommands() *cobra.Command {
	agents := &cobra.Command{
		Use:   "agents",
		Short: "Manage Wktbox skills for coding agents",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	agents.AddCommand(
		commands.agentInstall(),
		commands.agentStatus(),
		commands.agentUninstall(),
	)
	return agents
}

func (commands commandSet) agentInstall() *cobra.Command {
	return commands.agentMutation(
		"install",
		"Install or update the Wktbox agent skill",
		func(
			ctx context.Context,
			options agentintegration.Options,
		) (agentintegration.Report, error) {
			return commands.agents.Install(ctx, options)
		},
	)
}

func (commands commandSet) agentStatus() *cobra.Command {
	var target string
	command := &cobra.Command{
		Use:   "status",
		Short: "Inspect Wktbox agent skill installations",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options, err := agentOptions(target, false, false)
			if err != nil {
				return err
			}
			if commands.agents == nil {
				return errors.New("agent integration manager is not configured")
			}
			report, err := commands.agents.Status(command.Context(), options)
			if err != nil {
				return err
			}
			return commands.renderer().AgentReport(report)
		},
	}
	command.Flags().StringVar(
		&target,
		"target",
		string(agentintegration.TargetAll),
		"agent target: all, codex, or claude",
	)
	return command
}

type agentMutation func(
	context.Context,
	agentintegration.Options,
) (agentintegration.Report, error)

func (commands commandSet) agentMutation(
	use string,
	short string,
	mutate agentMutation,
) *cobra.Command {
	var target string
	var dryRun bool
	var force bool
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			options, err := agentOptions(target, dryRun, force)
			if err != nil {
				return err
			}
			if commands.agents == nil {
				return errors.New("agent integration manager is not configured")
			}
			report, err := mutate(command.Context(), options)
			if err != nil {
				return err
			}
			return commands.renderer().AgentReport(report)
		},
	}
	flags := command.Flags()
	flags.StringVar(
		&target,
		"target",
		string(agentintegration.TargetAll),
		"agent target: all, codex, or claude",
	)
	flags.BoolVar(&dryRun, "dry-run", false, "show changes without writing files")
	flags.BoolVar(&force, "force", false, "replace a locally modified managed skill")
	return command
}

func (commands commandSet) agentUninstall() *cobra.Command {
	return commands.agentMutation(
		"uninstall",
		"Remove the Wktbox agent skill and managed instructions",
		func(
			ctx context.Context,
			options agentintegration.Options,
		) (agentintegration.Report, error) {
			return commands.agents.Uninstall(ctx, options)
		},
	)
}

func agentOptions(
	target string,
	dryRun bool,
	force bool,
) (agentintegration.Options, error) {
	parsed := agentintegration.Target(target)
	switch parsed {
	case agentintegration.TargetAll,
		agentintegration.TargetCodex,
		agentintegration.TargetClaude:
	default:
		return agentintegration.Options{}, fmt.Errorf(
			"invalid agent target %q; expected all, codex, or claude",
			target,
		)
	}
	return agentintegration.Options{
		Target: parsed,
		DryRun: dryRun,
		Force:  force,
	}, nil
}

func (commands commandSet) up() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Create or start the external box infrastructure",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Ensure(command.Context(), resolution)
			if err != nil {
				return err
			}
			return commands.renderer().Box(box)
		},
	}
}

func (commands commandSet) run() *cobra.Command {
	return commands.child(
		"run -- <command> [args...]",
		"Ensure the box is ready and execute a child command",
		true,
		nil,
		true,
	)
}

func (commands commandSet) exec() *cobra.Command {
	return commands.child(
		"exec -- <command> [args...]",
		"Execute a child command in an already-ready box",
		false,
		nil,
		false,
	)
}

func (commands commandSet) compose() *cobra.Command {
	return commands.child(
		"compose -- <compose-args...>",
		"Run Docker Compose inside the isolated daemon",
		true,
		[]string{"docker", "compose"},
		true,
	)
}

func (commands commandSet) child(
	use string,
	short string,
	ensure bool,
	prefix []string,
	syncLoopback bool,
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			var box state.BoxRecord
			if ensure {
				box, err = commands.service.Ensure(command.Context(), resolution)
			} else {
				box, err = commands.service.Inspect(command.Context(), resolution)
				if err == nil && box.Status != state.Ready {
					return boxNotReadyError(box.Status)
				}
			}
			if err != nil {
				return err
			}
			child := append([]string{}, prefix...)
			child = append(child, arguments...)
			return commands.runChild(command.Context(), box, child, syncLoopback)
		},
	}
}

func (commands commandSet) shell() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive shell in /workspace",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Ensure(command.Context(), resolution)
			if err != nil {
				return err
			}
			code, err := commands.executeChild(command.Context(), box, []string{"bash"})
			if err != nil {
				return err
			}
			if code == 127 {
				code, err = commands.executeChild(command.Context(), box, []string{"sh"})
				if err != nil {
					return err
				}
			}
			return childResult(code)
		},
	}
}

func (commands commandSet) open() *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the known loopback Webtop URL",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Inspect(command.Context(), resolution)
			if err != nil {
				return err
			}
			if box.Status != state.Ready {
				return boxNotReadyError(box.Status)
			}
			if err := commands.service.Open(command.Context(), box); err != nil {
				return err
			}
			return commands.renderer().Message("Opened " + output.WebtopURL(box))
		},
	}
}

func (commands commandSet) list() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List and reconcile managed boxes",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			boxes, err := commands.service.List(command.Context())
			if err != nil {
				return err
			}
			return commands.renderer().Boxes(boxes)
		},
	}
}

func (commands commandSet) status() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the reconciled status of one box",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Inspect(command.Context(), resolution)
			if err != nil {
				return err
			}
			return commands.renderer().Box(box)
		},
	}
}

func (commands commandSet) logs() *cobra.Command {
	var follow bool
	result := &cobra.Command{
		Use:   "logs [service]",
		Short: "Stream external Compose service logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Inspect(command.Context(), resolution)
			if err != nil {
				return err
			}
			service := ""
			if len(arguments) == 1 {
				service = arguments[0]
			}
			code, err := commands.service.Logs(
				command.Context(),
				box,
				service,
				follow,
				commands.streams.Out,
				commands.streams.Err,
			)
			if err != nil {
				return err
			}
			return childResult(code)
		},
	}
	result.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return result
}

func (commands commandSet) stop() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop a box while preserving its volumes",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			if err := commands.service.Stop(command.Context(), resolution); err != nil {
				return err
			}
			return commands.renderer().Message("Box " + resolution.ID + " stopped.")
		},
	}
}

func (commands commandSet) restart() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart and revalidate a box",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			if err := commands.service.Restart(command.Context(), resolution); err != nil {
				return err
			}
			return commands.renderer().Message("Box " + resolution.ID + " restarted.")
		},
	}
}

func (commands commandSet) destroy() *cobra.Command {
	var force bool
	result := &cobra.Command{
		Use:   "destroy",
		Short: "Remove one box and its exclusive volumes",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			resolution, err := commands.resolve(command.Context())
			if err != nil {
				return err
			}
			box, err := commands.service.Inspect(command.Context(), resolution)
			if err != nil {
				return err
			}
			if !force {
				if !commands.streams.Terminal {
					return fmt.Errorf(
						"%w: box %s (%s); pass --force in non-interactive mode",
						ErrForceRequired,
						box.Name,
						box.ID,
					)
				}
				confirmed, err := commands.confirmDestroy(box)
				if err != nil {
					return err
				}
				if !confirmed {
					return errors.New("destroy cancelled")
				}
			}
			if err := commands.service.Destroy(command.Context(), resolution); err != nil {
				return err
			}
			return commands.renderer().Message("Box " + box.ID + " destroyed.")
		},
	}
	result.Flags().BoolVar(&force, "force", false, "destroy without interactive confirmation")
	return result
}

func (commands commandSet) doctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check host and project prerequisites",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			report, err := commands.service.Doctor(command.Context(), commands.request())
			if err != nil {
				return err
			}
			return commands.renderer().Value(report)
		},
	}
}

func (commands commandSet) resolve(ctx context.Context) (app.Resolution, error) {
	return commands.service.Resolve(ctx, commands.request())
}

func (commands commandSet) request() app.Request {
	return app.Request{
		Path:        commands.flags.path,
		ConfigPath:  commands.flags.configPath,
		EnvFile:     commands.flags.envFile,
		EnvTarget:   commands.flags.envTarget,
		Profile:     commands.flags.profile,
		Environment: append([]string(nil), commands.flags.environment...),
		Verbose:     commands.flags.verbose,
	}
}

func (commands commandSet) renderer() output.Renderer {
	return output.New(output.Options{
		JSON:  commands.flags.json,
		Quiet: commands.flags.quiet,
		Out:   commands.streams.Out,
		Err:   commands.streams.Err,
	})
}

func (commands commandSet) runChild(
	ctx context.Context,
	box state.BoxRecord,
	child []string,
	syncLoopback bool,
) error {
	code, err := commands.executeChild(ctx, box, child)
	if err != nil {
		return err
	}
	if resultErr := childResult(code); resultErr != nil {
		return resultErr
	}
	if !syncLoopback {
		return nil
	}
	status, err := commands.service.SyncLoopback(ctx, box)
	if err != nil {
		return err
	}
	return commands.renderer().LoopbackSummary(status)
}

func (commands commandSet) executeChild(
	ctx context.Context,
	box state.BoxRecord,
	child []string,
) (int, error) {
	return commands.service.Run(ctx, box, child, executor.Options{
		HostCWD:     commands.streams.WorkingDir,
		TTY:         commands.streams.Terminal,
		Environment: append([]string(nil), commands.flags.environment...),
		Stdin:       commands.streams.In,
		Stdout:      commands.streams.Out,
		Stderr:      commands.streams.Err,
	})
}

func (commands commandSet) confirmDestroy(box state.BoxRecord) (bool, error) {
	if _, err := fmt.Fprintf(
		commands.streams.Out,
		"Destroy box %s (%s), including its exclusive volumes? [y/N] ",
		box.Name,
		box.ID,
	); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(commands.streams.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func childResult(code int) error {
	if code == 0 {
		return nil
	}
	if code < 0 {
		code = 1
	}
	return ExitError{Code: code}
}

func boxNotReadyError(status state.Status) error {
	return fmt.Errorf(
		"box is %s; run \"wktbox up\" or use \"wktbox run -- <command>\"",
		status,
	)
}

func ErrorCode(err error) string {
	var exitError ExitError
	switch {
	case errors.As(err, &exitError):
		return "child_exit"
	case errors.Is(err, agentintegration.ErrConflict):
		return "agent_conflict"
	case errors.Is(err, ErrForceRequired):
		return "confirmation_required"
	case errors.Is(err, sandbox.ErrBoxNotFound):
		return "box_not_found"
	case errors.Is(err, sandbox.ErrSelectorNotFound):
		return "selector_not_found"
	case errors.Is(err, sandbox.ErrSelectorAmbiguous):
		return "selector_ambiguous"
	case errors.Is(err, sandbox.ErrConnectionNotFound):
		return "connection_not_found"
	case errors.Is(err, discovery.ErrInvalidWorkspace):
		return "invalid_workspace"
	case errors.Is(err, discovery.ErrNotWorktree):
		return "not_a_worktree"
	default:
		return "runtime_error"
	}
}

func boxRecordsByID(boxes []state.BoxRecord) map[string]state.BoxRecord {
	result := make(map[string]state.BoxRecord, len(boxes))
	for _, box := range boxes {
		result[box.ID] = box
	}
	return result
}
