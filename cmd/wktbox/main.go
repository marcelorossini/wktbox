package main

import (
	"errors"
	"os"

	"wktbox/internal/agentintegration"
	"wktbox/internal/app"
	"wktbox/internal/cli"
	"wktbox/internal/output"
	"wktbox/internal/version"
)

func main() {
	streams := cli.Streams{
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
		Terminal: isTerminal(os.Stdin) && isTerminal(os.Stdout),
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		streams.WorkingDir = workingDirectory
	}

	application, err := app.NewDefault()
	if err != nil {
		renderer := output.New(output.Options{
			JSON: containsArgument(os.Args[1:], "--json"),
			Out:  streams.Out,
			Err:  streams.Err,
		})
		_ = renderer.Error("initialization_error", err.Error())
		os.Exit(1)
	}
	agents := agentintegration.NewManager(agentintegration.Dependencies{
		Version: version.String(),
	})
	os.Exit(run(cli.Dependencies{
		Service: application,
		Agents:  agents,
	}, streams, os.Args[1:]))
}

func run(
	dependencies cli.Dependencies,
	streams cli.Streams,
	arguments []string,
) int {
	root := cli.New(dependencies, streams)
	root.SetArgs(arguments)
	err := root.Execute()
	if err == nil {
		return 0
	}

	var exitError cli.ExitError
	if errors.As(err, &exitError) {
		return exitError.Code
	}
	jsonOutput, _ := root.PersistentFlags().GetBool("json")
	renderer := output.New(output.Options{
		JSON: jsonOutput,
		Out:  streams.Out,
		Err:  streams.Err,
	})
	_ = renderer.Error(cli.ErrorCode(err), err.Error())
	return 1
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func containsArgument(arguments []string, target string) bool {
	for _, argument := range arguments {
		if argument == target {
			return true
		}
	}
	return false
}
