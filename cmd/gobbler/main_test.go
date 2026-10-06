package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunRoutesToRegisteredCommand(t *testing.T) {
	var got []string
	cmds := []command{
		{name: "alpha", summary: "first", run: func(_ context.Context, args []string) error {
			got = append([]string{"alpha"}, args...)
			return nil
		}},
		{name: "beta", summary: "second", run: func(_ context.Context, args []string) error {
			got = append([]string{"beta"}, args...)
			return nil
		}},
	}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"first command", []string{"alpha"}, []string{"alpha"}},
		{"second command with args", []string{"beta", "-x", "y"}, []string{"beta", "-x", "y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got = nil
			if err := run(context.Background(), tt.args, cmds, &bytes.Buffer{}); err != nil {
				t.Fatalf("run: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Fatalf("routed %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunUnknownCommandNamesItAndPrintsUsage(t *testing.T) {
	cmds := []command{{name: "alpha", summary: "first", run: func(context.Context, []string) error { return nil }}}
	var out bytes.Buffer
	err := run(context.Background(), []string{"bogus"}, cmds, &out)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want error naming bogus", err)
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("usage %q does not list registered commands", out.String())
	}
}

func TestRunNoCommandPrintsUsageAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), nil, nil, &out); err == nil {
		t.Fatal("want error with no subcommand")
	}
	if !strings.Contains(out.String(), "Usage") {
		t.Fatalf("usage %q missing", out.String())
	}
}

func TestRunPropagatesCommandError(t *testing.T) {
	boom := errors.New("boom")
	cmds := []command{{name: "x", run: func(context.Context, []string) error { return boom }}}
	err := run(context.Background(), []string{"x"}, cmds, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
}

func TestExecuteExitCodes(t *testing.T) {
	ok := []command{{name: "ok", run: func(context.Context, []string) error { return nil }}}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"ok"}, 0},
		{"unknown command", []string{"nope"}, 1},
		{"no command", nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execute(context.Background(), tt.args, ok, &bytes.Buffer{}); got != tt.want {
				t.Fatalf("exit = %d, want %d", got, tt.want)
			}
		})
	}
}
