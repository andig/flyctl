package machine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jpillora/backoff"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/flaps"
)

func WaitForStartOrStop(ctx context.Context, machine *api.Machine, action string, timeout time.Duration) error {
	var flapsClient = flaps.FromContext(ctx)

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var waitOnAction string
	switch action {
	case "start":
		waitOnAction = "started"
	case "stop":
		waitOnAction = "stopped"
	default:
		return fmt.Errorf("action must be either start or stop")
	}

	b := &backoff.Backoff{
		Min:    500 * time.Millisecond,
		Max:    2 * time.Second,
		Factor: 2,
		Jitter: false,
	}
	for {
		err := flapsClient.Wait(waitCtx, machine, waitOnAction, 60*time.Second)
		if err == nil {
			return nil
		}

		switch {
		case errors.Is(waitCtx.Err(), context.Canceled):
			return err
		case errors.Is(waitCtx.Err(), context.DeadlineExceeded):
			return fmt.Errorf("timeout reached waiting for machine to %s %w", waitOnAction, err)
		default:
			var flapsErr *flaps.FlapsError
			if errors.As(err, &flapsErr) && flapsErr.ResponseStatusCode == http.StatusBadRequest {
				return fmt.Errorf("failed waiting for machine: %w", err)
			}
			time.Sleep(b.Duration())
		}
	}
}
