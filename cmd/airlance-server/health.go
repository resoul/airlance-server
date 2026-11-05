package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func healthcheck(ctx context.Context) error {
	addr := os.Getenv("AIRLANCE_SERVER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}

	if _, port, found := strings.Cut(addr, ":"); found {
		addr = "127.0.0.1:" + port
	}

	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health/ready", http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.New("not ready")
	}

	return nil
}
