// Command gobbler is the single entry point for the recipe manager.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// command is one subcommand of the gobbler binary.
type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string) error
}

func main() {
	os.Exit(execute(os.Args[1:], commands(), os.Stderr))
}

// execute is main without the process exit: it installs the JSON logger on
// stderr and a signal-cancelled context, dispatches, and returns the exit
// code. A failure is logged as one JSON line.
func execute(args []string, cmds []command, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, args, cmds, stderr); err != nil {
		logger.Error("gobbler failed", "error", err.Error())
		return 1
	}
	return 0
}

// run dispatches args[0] to the matching command. With no or an unknown
// subcommand it writes usage to out and returns an error.
func run(ctx context.Context, args []string, cmds []command, out io.Writer) error {
	if len(args) == 0 {
		usage(out, cmds)
		return fmt.Errorf("no subcommand given")
	}
	for _, c := range cmds {
		if c.name == args[0] {
			if err := c.run(ctx, args[1:]); err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			return nil
		}
	}
	usage(out, cmds)
	return fmt.Errorf("unknown subcommand %q", args[0])
}

// usage writes to the diagnostic stream; a failed write there has nowhere
// better to be reported, so write errors are deliberately dropped.
func usage(out io.Writer, cmds []command) {
	_, _ = fmt.Fprintln(out, "Usage: gobbler <command> [args]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Commands:")
	for _, c := range cmds {
		_, _ = fmt.Fprintf(out, "  %-12s %s\n", c.name, c.summary)
	}
}
