package main

import (
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
