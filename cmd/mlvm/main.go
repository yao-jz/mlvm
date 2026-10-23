package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/yao-jz/mlvm/rvgo/cmd"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	app := cli.NewApp()
	app.Name = "mlvm"
	app.Usage = "deterministic RISC-V execution and proof toolkit"
	app.Description = "Execute RV64IMAFD programs over committed memory, inspect traces, and generate single-step witnesses."
	app.Version = fmt.Sprintf("%s (%s)", version, commit)
	app.Commands = []*cli.Command{
		cmd.LoadELFCommand,
		cmd.WitnessCommand,
		cmd.RunCommand,
		cmd.DemoCommand,
		cmd.XrayCommand,
		cmd.LiarCommand,
		cmd.RefereeCommand,
		cmd.CapabilitiesCommand,
		cmd.SoftFloatVectorsCommand,
		cmd.FloatSubGameFixtureCommand,
		cmd.MLClaimCommand,
	}
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
		fmt.Fprintln(os.Stderr, "\ncommand interrupted")
		// `run` stops at its next context check; long loops that do not watch the context,
		// such as `liar` and `xray` on a billion-instruction guest, would keep going. Give
		// the first kind a moment to stop cleanly, then exit.
		time.Sleep(2 * time.Second)
		os.Exit(130)
	}()

	err := app.RunContext(ctx, cmd.HoistFlags(app.Commands, os.Args))
	if err != nil {
		if errors.Is(err, ctx.Err()) {
			os.Exit(130)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
