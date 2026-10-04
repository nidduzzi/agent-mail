package main

import (
	"io"
	"os"
	"testing"
	"time"
)

func joinedWatcher(tb testing.TB) (*watcher, time.Time) {
	tb.Helper()
	dir := tb.TempDir()
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	a := agent{
		cfg: config{
			self:              "beta",
			dir:               dir,
			syncCheckInterval: time.Minute,
			pollInterval:      time.Second,
			staleAfter:        30 * time.Minute,
		},
		box:   mailbox{dir: dir},
		now:   func() time.Time { return start },
		host:  "test-host",
		stdin: nil,
		out:   newOutput(io.Discard),
		quiet: newOutput(io.Discard),
	}
	if err := a.createMailbox([]string{"--deadline", "2031-01-01 00:00"}); err != nil {
		tb.Fatal(err)
	}
	if err := a.join([]string{"beta"}); err != nil {
		tb.Fatal(err)
	}
	w, err := newWatcher(a, start)
	if err != nil {
		tb.Fatal(err)
	}
	return w, start
}

func TestPollAnnouncesOnlyNewMail(t *testing.T) {
	w, now := joinedWatcher(t)
	var announced testWriter
	w.agent.out = newOutput(&announced)
	if err := os.WriteFile(w.inbox+"/one.md", []byte("---\nfrom: alpha\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(w.inbox, future, future); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := w.poll(now.Add(time.Duration(i) * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := string(announced), "NEW MAIL: one.md from alpha\n"; got != want {
		t.Fatalf("announced %q, want %q", got, want)
	}
}

func TestPollStopsAtTheDeadline(t *testing.T) {
	w, _ := joinedWatcher(t)
	err := w.poll(time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, dead := err.(deadError); !dead {
		t.Fatalf("poll at the deadline returned %v, want a deadError", err)
	}
}

func BenchmarkQuietPoll(b *testing.B) {
	w, now := joinedWatcher(b)
	if err := w.poll(now); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.poll(now.Add(time.Second)); err != nil {
			b.Fatal(err)
		}
	}
}

type testWriter []byte

func (t *testWriter) Write(p []byte) (int, error) {
	*t = append(*t, p...)
	return len(p), nil
}
