// Package logs implements the logs command chain.
package logs

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/azazeal/pause"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/iostreams"
	"github.com/superfly/flyctl/logs"

	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/config"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/logger"
	"github.com/superfly/flyctl/internal/render"
)

func New() (cmd *cobra.Command) {
	const (
		long = `View application logs as generated by the application running on
the Fly platform.

Logs can be filtered to a specific instance using the --instance/-i flag or
to all instances running in a specific region using the --region/-r flag.
`
		short = "View app logs"
	)

	cmd = command.New("logs", short, long, run,
		command.RequireSession,
		command.RequireAppName,
	)

	cmd.Args = cobra.NoArgs

	flag.Add(cmd,
		flag.App(),
		flag.AppConfig(),
		flag.Region(),
		flag.String{
			Name:        "instance",
			Shorthand:   "i",
			Description: "Filter by instance ID",
		},
	)

	return
}

func run(ctx context.Context) error {
	client := client.FromContext(ctx).API()

	opts := &logs.LogOptions{
		AppName:    appconfig.NameFromContext(ctx),
		RegionCode: config.FromContext(ctx).Region,
		VMID:       flag.GetString(ctx, "instance"),
	}

	var eg *errgroup.Group
	eg, ctx = errgroup.WithContext(ctx)

	// pollingCtx, cancelPolling := context.WithCancel(ctx)
	// pollEntries := poll(pollingCtx, eg, client, opts)
	liveEntries := nats(ctx, eg, client, opts, func() {})

	eg.Go(func() error {
		return printStreams(ctx, liveEntries)
	})

	return eg.Wait()
}

func poll(ctx context.Context, eg *errgroup.Group, client *api.Client, opts *logs.LogOptions) <-chan logs.LogEntry {
	c := make(chan logs.LogEntry)

	eg.Go(func() (err error) {
		defer close(c)

		if err = logs.Poll(ctx, c, client, opts); errors.Is(err, context.Canceled) {
			// if the parent context is cancelled then the errorgroup will return
			// context.Canceled because nats and/or printStreams will return it.
			err = nil
		}

		return
	})

	return c
}

func nats(ctx context.Context, eg *errgroup.Group, client *api.Client, opts *logs.LogOptions, cancelPolling context.CancelFunc) <-chan logs.LogEntry {
	c := make(chan logs.LogEntry)

	eg.Go(func() error {
		defer close(c)

		stream, err := logs.NewNatsStream(ctx, client, opts)
		if err != nil {
			logger := logger.FromContext(ctx)

			logger.Debugf("could not connect to wireguard tunnel: %v\n", err)
			logger.Debug("falling back to log polling...")

			return nil
		}

		// we wait for 2 seconds before canceling the polling context so that
		// we get a few records
		pause.For(ctx, 2*time.Second)
		cancelPolling()

		for entry := range stream.Stream(ctx, opts) {
			c <- entry
		}

		return nil
	})

	return c
}

func printStreams(ctx context.Context, streams ...<-chan logs.LogEntry) error {
	var eg *errgroup.Group
	eg, ctx = errgroup.WithContext(ctx)

	out := iostreams.FromContext(ctx).Out
	json := config.FromContext(ctx).JSONOutput

	for _, stream := range streams {
		stream := stream

		eg.Go(func() error {
			return printStream(ctx, out, stream, json)
		})
	}

	return eg.Wait()
}

func printStream(ctx context.Context, w io.Writer, stream <-chan logs.LogEntry, json bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, ok := <-stream:
			if !ok {
				return nil
			}

			var err error
			if json {
				err = render.JSON(w, entry)
			} else {
				err = render.LogEntry(w, entry,
					render.HideAllocID(),
					render.RemoveNewlines(),
					render.HideRegion(),
				)
			}

			if err != nil {
				return err
			}
		}
	}
}
