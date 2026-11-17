package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurationFlagAcceptsSecondsAndDurations(t *testing.T) {
	old := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = old })

	parse := func(args ...string) (time.Duration, error) {
		flag.CommandLine = flag.NewFlagSet("lqworker", flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		var d time.Duration
		durationFlag(&d, "sleep", "usage")
		return d, flag.CommandLine.Parse(args)
	}

	for arg, want := range map[string]time.Duration{
		"--sleep=3":     3 * time.Second, // as in "php artisan queue:work --sleep=3"
		"--sleep=3s":    3 * time.Second,
		"--sleep=0.5":   500 * time.Millisecond,
		"--sleep=250ms": 250 * time.Millisecond,
	} {
		got, err := parse(arg)
		require.NoError(t, err, arg)
		assert.Equal(t, want, got, arg)
	}

	_, err := parse("--sleep=soon")
	assert.Error(t, err)
}

func runShutdown(t *testing.T, timeout time.Duration, script func(signals chan<- os.Signal)) (stopped, aborted time.Duration) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		signals := make(chan os.Signal, 2)
		start := time.Now()
		stopped, aborted = -1, -1
		done := make(chan struct{})

		go func() {
			defer close(done)
			shutdownOnSignal(signals,
				func() { stopped = time.Since(start) },
				func() { aborted = time.Since(start) },
				timeout, slog.New(slog.DiscardHandler))
		}()

		script(signals)
		<-done
	})
	return stopped, aborted
}

func TestShutdownFirstSignalDrainsSecondAborts(t *testing.T) {
	stopped, aborted := runShutdown(t, time.Hour, func(signals chan<- os.Signal) {
		time.Sleep(10 * time.Second)
		signals <- syscall.SIGTERM
		time.Sleep(5 * time.Second)
		signals <- syscall.SIGINT
	})

	assert.Equal(t, 10*time.Second, stopped, "the first signal only stops the worker from taking new jobs")
	assert.Equal(t, 15*time.Second, aborted, "the second signal cancels running jobs")
}

func TestShutdownTimeoutAborts(t *testing.T) {
	stopped, aborted := runShutdown(t, 30*time.Second, func(signals chan<- os.Signal) {
		signals <- syscall.SIGTERM
	})

	assert.Equal(t, time.Duration(0), stopped)
	assert.Equal(t, 30*time.Second, aborted)
}

func TestShutdownWithoutTimeoutWaitsForSecondSignal(t *testing.T) {
	stopped, aborted := runShutdown(t, 0, func(signals chan<- os.Signal) {
		signals <- syscall.SIGTERM
		time.Sleep(24 * time.Hour)
		signals <- syscall.SIGTERM
	})

	assert.Equal(t, time.Duration(0), stopped)
	assert.Equal(t, 24*time.Hour, aborted)
}
