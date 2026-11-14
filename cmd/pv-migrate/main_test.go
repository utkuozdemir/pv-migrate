package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchSignalStopsCatchingAfterFirstSignal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	doneCh := make(chan struct{})

	defer close(doneCh)

	var stoppedCh chan<- os.Signal

	stopped := false
	stop := func(ch chan<- os.Signal) {
		stopped = true
		stoppedCh = ch
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		watchSignal(signalCh, doneCh, stop, cancel)
	}()

	signalCh <- os.Interrupt

	<-done

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.True(t, stopped)
	assert.Equal(t, (chan<- os.Signal)(signalCh), stoppedCh)
}

func TestWatchSignalExitsOnDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	doneCh := make(chan struct{})

	stopped := false
	stop := func(chan<- os.Signal) {
		stopped = true
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		watchSignal(signalCh, doneCh, stop, cancel)
	}()

	close(doneCh)

	<-done

	require.NoError(t, ctx.Err())
	assert.False(t, stopped)
}
