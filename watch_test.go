package main

import (
	"os"
	"testing"
	"time"
)

func joinedWatcher(tb testing.TB) (*watcher, time.Time) {
	tb.Helper()
	start := testStart
	a := newTestAgent(tb, &start, "2031-01-01 00:00").as("beta")
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

func TestRescanFindsMailThatKeptTheInboxTime(t *testing.T) {
	w, now := joinedWatcher(t)
	var announced testWriter
	w.agent.out = newOutput(&announced)
	if err := w.poll(now); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(w.inbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.inbox+"/synced.md", []byte("---\nfrom: alpha\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(w.inbox, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Duration{time.Second, fullRescanEvery, fullRescanEvery + time.Second, 2*fullRescanEvery + time.Second} {
		if err := w.poll(now.Add(at)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := string(announced), "NEW MAIL: synced.md from alpha\n"; got != want {
		t.Fatalf("announced %q, want %q", got, want)
	}
}

func TestPollDiesWhenTheInboxIsGone(t *testing.T) {
	w, now := joinedWatcher(t)
	if err := os.RemoveAll(w.inbox); err != nil {
		t.Fatal(err)
	}
	err := w.poll(now.Add(time.Second))
	if _, dead := err.(deadError); !dead {
		t.Fatalf("poll without an inbox returned %v, want a deadError", err)
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
