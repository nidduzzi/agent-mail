package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	watchDeliver byte = iota
	watchAck
	watchPoll
	watchRestart
	watchDeadline
	watchLeave
	watchDropInbox
	watchOps
)

var watchOpNames = [watchOps]string{"deliver", "ack", "poll", "restart", "deadline", "leave", "drop-inbox"}

const (
	deliverMessage byte = iota
	deliverHidden
	deliverNotMarkdown
	deliverDirectory
	deliverForgedSender
	deliverKinds
)

var pollAdvances = [...]time.Duration{0, time.Second, 30 * time.Second, fullRescanEvery - time.Second, fullRescanEvery, fullRescanEvery + time.Second, presenceRefresh, presenceRefresh + time.Second}

type watchStep struct {
	op        byte
	argument  byte
	touchesMt bool
}

func (s watchStep) String() string {
	return watchOpNames[s.op] + "(" + strconv.Itoa(int(s.argument)) + "," + strconv.FormatBool(s.touchesMt) + ")"
}

func deliver(kind byte, bumpsInboxTime bool) watchStep {
	return watchStep{watchDeliver, kind, bumpsInboxTime}
}
func ackOldest(bumpsInboxTime bool) watchStep { return watchStep{watchAck, 0, bumpsInboxTime} }
func poll(advance int) watchStep              { return watchStep{watchPoll, byte(advance), false} }
func restartWatch() watchStep                 { return watchStep{watchRestart, 0, false} }
func moveDeadline(passed bool) watchStep      { return watchStep{watchDeadline, 0, passed} }
func leaveWhileWatching() watchStep           { return watchStep{watchLeave, 0, false} }
func dropInbox() watchStep                    { return watchStep{watchDropInbox, 0, false} }

const noAdvance, aSecond, halfMinute, justBeforeRescan, atRescan, afterRescan, atPresence, afterPresence = 0, 1, 2, 3, 4, 5, 6, 7

func encodeWatch(steps ...watchStep) []byte {
	encoded := make([]byte, 0, 3*len(steps))
	for _, s := range steps {
		touches := byte(0)
		if s.touchesMt {
			touches = 1
		}
		encoded = append(encoded, s.op, s.argument, touches)
	}
	return encoded
}

func decodeWatch(encoded []byte) []watchStep {
	steps := make([]watchStep, 0, len(encoded)/3)
	for ; len(encoded) >= 3; encoded = encoded[3:] {
		steps = append(steps, watchStep{encoded[0] % watchOps, encoded[1], encoded[2]&1 == 1})
	}
	return steps
}

var pinnedWatchSequences = map[string][]watchStep{
	"new mail is announced once":                       {deliver(deliverMessage, true), poll(aSecond), poll(aSecond), poll(afterRescan)},
	"mail present at start is not announced":           {deliver(deliverMessage, true), restartWatch(), poll(aSecond), poll(afterRescan)},
	"mail that kept the inbox time waits for a rescan": {poll(noAdvance), deliver(deliverMessage, false), poll(aSecond), poll(justBeforeRescan), poll(aSecond)},
	"the rescan is due exactly a minute later":         {poll(noAdvance), deliver(deliverMessage, false), poll(atRescan)},
	"only markdown messages are announced":             {deliver(deliverHidden, true), deliver(deliverNotMarkdown, true), deliver(deliverDirectory, true), poll(aSecond), deliver(deliverMessage, true), poll(aSecond)},
	"a forged sender is shown as unknown":              {deliver(deliverForgedSender, true), poll(aSecond)},
	"mail after the inbox is gone changes nothing":     {dropInbox(), deliver(deliverMessage, true), ackOldest(true), poll(aSecond)},
	"mail after leaving changes nothing":               {deliver(deliverMessage, true), leaveWhileWatching(), ackOldest(true), poll(aSecond)},
	"acked mail announced again only if it returns":    {deliver(deliverMessage, true), poll(aSecond), ackOldest(true), poll(aSecond), deliver(deliverMessage, true), poll(aSecond)},
	"presence refreshes every five minutes":            {poll(noAdvance), poll(atPresence), poll(aSecond), poll(afterPresence), poll(atPresence)},
	"the watcher stops at the deadline":                {poll(aSecond), moveDeadline(true), poll(aSecond), poll(aSecond)},
	"a moved deadline in the future keeps it alive":    {poll(aSecond), moveDeadline(false), poll(afterRescan)},
	"the watcher stops when its member leaves":         {poll(aSecond), leaveWhileWatching(), leaveWhileWatching(), poll(aSecond)},
	"the watcher stops when its inbox is gone":         {poll(aSecond), dropInbox(), poll(aSecond)},
	"a watcher cannot start without its inbox":         {dropInbox(), restartWatch()},
	"several arrivals in one scan":                     {deliver(deliverMessage, false), deliver(deliverMessage, false), deliver(deliverMessage, true), poll(aSecond)},
}

type watchModel struct {
	now          time.Time
	inbox        []string
	announced    []string
	inboxDirty   bool
	nextRescan   time.Time
	seen         time.Time
	nextPresence time.Time
	deadline     time.Time
	ended        bool
	delivered    int
	fakeTime     time.Time
}

func FuzzWatcher(f *testing.F) {
	for _, steps := range pinnedWatchSequences {
		f.Add(encodeWatch(steps...))
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		m := &watchModel{now: testStart, seen: testStart, nextPresence: testStart, deadline: time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC), fakeTime: testStart.Add(-time.Hour)}
		a := newTestAgent(t, &m.now, "2031-01-01 00:00").as("beta")
		if err := a.join([]string{"beta"}); err != nil {
			t.Fatal(err)
		}
		var announced bytes.Buffer
		a.out = newOutput(&announced)
		w := m.restart(t, a)
		for _, s := range decodeWatch(encoded) {
			if m.ended {
				return
			}
			w = m.apply(t, a, w, s, &announced)
		}
	})
}

func (m *watchModel) uniqueTime() time.Time {
	m.fakeTime = m.fakeTime.Add(time.Second)
	return m.fakeTime
}

func (m *watchModel) setInboxTime(t *testing.T, inbox string, before time.Time, bump bool) {
	t.Helper()
	at := before
	if bump {
		at = m.uniqueTime()
		m.inboxDirty = true
	}
	if err := os.Chtimes(inbox, at, at); err != nil {
		t.Fatal(err)
	}
}

func (m *watchModel) restart(t *testing.T, a agent) *watcher {
	t.Helper()
	w, err := newWatcher(a, m.now)
	if err != nil {
		t.Fatal(err)
	}
	m.announced = slices.Clone(m.inbox)
	m.inboxDirty = true
	m.nextRescan = time.Time{}
	m.nextPresence = m.now
	return w
}

func (m *watchModel) apply(t *testing.T, a agent, w *watcher, s watchStep, announced *bytes.Buffer) *watcher {
	t.Helper()
	inbox := a.box.inbox("beta")
	if (s.op == watchDeliver || s.op == watchAck) && (!isDir(inbox) || !isFile(a.box.memberFile("beta"))) {
		return w
	}
	switch s.op {
	case watchDeliver:
		before := modTime(t, inbox)
		m.delivered++
		name := "m" + strconv.Itoa(1000+m.delivered) + ".md"
		switch s.argument % deliverKinds {
		case deliverMessage:
			writeFile(t, filepath.Join(inbox, name), "---\nid: x\nfrom: alpha\nto: beta\n---\nhi\n")
			m.inbox = append(m.inbox, name)
		case deliverForgedSender:
			writeFile(t, filepath.Join(inbox, name), "---\nfrom: ../mallory\n---\nhi\n")
			m.inbox = append(m.inbox, name)
		case deliverHidden:
			writeFile(t, filepath.Join(inbox, "."+name+".tmp"), "partial")
		case deliverNotMarkdown:
			writeFile(t, filepath.Join(inbox, name+".txt"), "note")
		case deliverDirectory:
			if err := os.Mkdir(filepath.Join(inbox, "dir-"+name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		m.setInboxTime(t, inbox, before, s.touchesMt)
	case watchAck:
		if len(m.inbox) == 0 {
			return w
		}
		before := modTime(t, inbox)
		slices.Sort(m.inbox)
		if err := a.acknowledge([]string{m.inbox[0]}); err != nil {
			t.Fatal(err)
		}
		m.inbox = m.inbox[1:]
		m.setInboxTime(t, inbox, before, s.touchesMt)
		if m.now.Sub(m.seen) >= presenceRefresh {
			m.seen = m.now
		}
	case watchRestart:
		if !isDir(inbox) {
			_, err := newWatcher(a, m.now)
			if _, dead := err.(deadError); !dead || !strings.Contains(err.Error(), "is gone") {
				t.Fatalf("%s without an inbox returned %v, want a deadError", s, err)
			}
			m.ended = true
			return w
		}
		return m.restart(t, a)
	case watchDeadline:
		text := "2031-01-01 00:00"
		m.deadline = time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
		if s.touchesMt {
			text, m.deadline = m.now.Add(time.Second).Format(deadlineLayout), m.now.Add(time.Second).Truncate(time.Minute)
		}
		writeFile(t, a.box.deadlineFile(), text+"\n")
		at := m.uniqueTime()
		if err := os.Chtimes(a.box.deadlineFile(), at, at); err != nil {
			t.Fatal(err)
		}
	case watchLeave:
		if err := os.Remove(a.box.memberFile("beta")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	case watchDropInbox:
		if err := os.RemoveAll(inbox); err != nil {
			t.Fatal(err)
		}
	case watchPoll:
		m.now = m.now.Add(pollAdvances[int(s.argument)%len(pollAdvances)])
		announced.Reset()
		err := w.poll(m.now)
		m.expectPoll(t, a, s, err, announced.String())
	}
	return w
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func (m *watchModel) expectPoll(t *testing.T, a agent, s watchStep, err error, printed string) {
	t.Helper()
	_, dead := err.(deadError)
	switch {
	case !m.now.Before(m.deadline):
		m.ended = true
		if !dead || !strings.Contains(err.Error(), "deadline passed") {
			t.Fatalf("%s at the deadline returned %v, want a deadError", s, err)
		}
		return
	case !isFile(a.box.memberFile("beta")):
		m.ended = true
		if err == nil || !strings.Contains(err.Error(), "no longer a member") {
			t.Fatalf("%s after leaving returned %v", s, err)
		}
		return
	case !isDir(a.box.inbox("beta")):
		m.ended = true
		if !dead || !strings.Contains(err.Error(), "is gone") {
			t.Fatalf("%s without an inbox returned %v, want a deadError", s, err)
		}
		return
	case err != nil:
		t.Fatalf("%s: %v", s, err)
	}

	var want strings.Builder
	if m.inboxDirty || !m.now.Before(m.nextRescan) {
		current := slices.Clone(m.inbox)
		slices.Sort(current)
		for _, name := range current {
			if !slices.Contains(m.announced, name) {
				content, _ := os.ReadFile(filepath.Join(a.box.inbox("beta"), name))
				sender := "alpha"
				if strings.Contains(string(content), "mallory") {
					sender = "(unknown sender)"
				}
				want.WriteString("NEW MAIL: " + name + " from " + sender + "\n")
			}
		}
		m.announced = current
		m.inboxDirty = false
		m.nextRescan = m.now.Add(fullRescanEvery)
	}
	if printed != want.String() {
		t.Fatalf("%s: announced %q, want %q", s, printed, want.String())
	}
	if !m.now.Before(m.nextPresence) {
		if m.now.Sub(m.seen) >= presenceRefresh {
			m.seen = m.now
		}
		m.nextPresence = m.now.Add(presenceRefresh)
	}
	member, err := readMember(a.box, "beta")
	if err != nil || !member.seen.Equal(m.seen.Truncate(time.Second)) {
		t.Fatalf("%s: beta last seen %v (%v), want %v", s, member.seen, err, m.seen)
	}
}
