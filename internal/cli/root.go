package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"wktbox/internal/app"
	"wktbox/internal/discovery"
	"wktbox/internal/executor"
	"wktbox/internal/output"
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
	Run(context.Context, state.BoxRecord, []string, executor.Options) (int, error)
	Logs(context.Context, state.BoxRecord, string, bool, io.Writer, io.Writer) (int, error)
	Stop(context.Context, app.Resolution) error
	Restart(context.Context, app.Resolution) error
	Destroy(context.Context, app.Resolution) error
	Open(context.Context, state.BoxRecord) error
	Doctor(context.Context, app.Request) (any, error)
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
	streams Streams
	flags   *flags
}

func New(service Service, streams Streams) *cobra.Command {
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
		service: service,
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
		commands.logs(),
		commands.stop(),
		commands.restart(),
		commands.destroy(),
		commands.doctor(),
	)
	return root
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
	)
}

func (commands commandSet) exec() *cobra.Command {
	return commands.child(
		"exec -- <command> [args...]",
		"Execute a child command in an already-ready box",
		false,
		nil,
	)
}

func (commands commandSet) compose() *cobra.Command {
	return commands.child(
		"compose -- <compose-args...>",
		"Run Docker Compose inside the isolated daemon",
		true,
		[]string{"docker", "compose"},
	)
}

func (commands commandSet) child(
	use string,
	short string,
	ensure bool,
	prefix []string,
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
			return commands.runChild(command.Context(), box, child)
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
) error {
	code, err := commands.executeChild(ctx, box, child)
	if err != nil {
		return err
	}
	return childResult(code)
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
	case errors.Is(err, ErrForceRequired):
		return "confirmation_required"
	case errors.Is(err, sandbox.ErrBoxNotFound):
		return "box_not_found"
	case errors.Is(err, discovery.ErrNotWorktree):
		return "not_a_worktree"
	default:
		return "runtime_error"
	}
}
