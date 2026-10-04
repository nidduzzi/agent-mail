package main

import (
	"io"
	"testing"
	"time"
)

var testStart = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func newTestAgent(tb testing.TB, clock *time.Time, deadline string) agent {
	tb.Helper()
	dir := tb.TempDir()
	a := agent{
		cfg: config{
			dir:               dir,
			syncCheckInterval: time.Minute,
			pollInterval:      time.Second,
			staleAfter:        30 * time.Minute,
		},
		box:   mailbox{dir: dir},
		now:   func() time.Time { return *clock },
		host:  "test-host",
		stdin: nil,
		out:   newOutput(io.Discard),
		quiet: newOutput(io.Discard),
	}
	if err := a.createMailbox([]string{"--deadline", deadline}); err != nil {
		tb.Fatal(err)
	}
	return a
}

func (a agent) as(self string) agent {
	a.cfg.self = self
	return a
}
