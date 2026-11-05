package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) error {
	t.Helper()

	root := newRootCommand(slog.New(slog.NewTextHandler(io.Discard, nil)))
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	return root.Execute()
}

func TestDangerousCommandsNeedConfirmation(t *testing.T) {
	if err := run(t, "migrate", "reset"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("reset without --force: %v", err)
	}

	t.Setenv("AIRLANCE_SEED", "")

	if err := run(t, "seed", "run"); err == nil || !strings.Contains(err.Error(), "AIRLANCE_SEED") {
		t.Fatalf("seed without permission: %v", err)
	}
}

func TestUnknownInputIsRefused(t *testing.T) {
	for _, args := range [][]string{{"nonsense"}, {"migrate"}, {"migrate", "sideways"}, {"seed", "run", "--unknown"}} {
		if err := run(t, args...); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

func TestCreateMigrationNumbersThePair(t *testing.T) {
	directory := t.TempDir()

	for _, name := range []string{"000001_initial.up.sql", "000001_initial.down.sql", "000007_more.up.sql", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := createMigration(directory, "add_things"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"000008_add_things.up.sql", "000008_add_things.down.sql"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Error(err)
		}
	}

	if err := createMigration(directory, "Bad Name"); err == nil {
		t.Fatal("a bad name was accepted")
	}
}
