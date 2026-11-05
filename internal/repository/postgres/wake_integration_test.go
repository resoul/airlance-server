//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"go.airlance.org/server/internal/repository/postgres"
)

func TestAnotherProcessCanWakeTheServer(t *testing.T) {
	parent, pool := openMessages(t)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	wakes := postgres.NewWakes(pool, func(err error) {
		if ctx.Err() == nil {
			t.Error(err)
		}
	})
	heard := make(chan string, 1)

	go wakes.Run(ctx, func(subject string) {
		heard <- subject
	})

	deadline := time.After(5 * time.Second)
	retry := time.NewTicker(50 * time.Millisecond)

	defer retry.Stop()

	for {
		select {
		case subject := <-heard:
			if subject != "anna" {
				t.Fatalf("got %q", subject)
			}

			return
		case <-retry.C:
			wakes.Notify("anna")
		case <-deadline:
			t.Fatal("nobody heard")
		}
	}
}
