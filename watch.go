package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const fullRescanEvery = time.Minute

type watcher struct {
	agent        agent
	inbox        string
	deadlineFile string
	memberFile   string
	announced    map[string]uint32
	scan         uint32
	inboxChanged time.Time
	nextRescan   time.Time
	deadline     time.Time
	hasDeadline  bool
	deadlineMod  time.Time
	nextSync     time.Time
	nextPresence time.Time
}

func (a agent) watch() error {
	w, err := newWatcher(a, a.now())
	if err != nil {
		return err
	}
	for {
		if err := w.poll(a.now()); err != nil {
			return err
		}
		time.Sleep(a.cfg.pollInterval)
	}
}

func newWatcher(a agent, now time.Time) (*watcher, error) {
	w := &watcher{
		agent:        a,
		inbox:        a.box.inbox(a.cfg.self),
		deadlineFile: a.box.deadlineFile(),
		memberFile:   a.box.memberFile(a.cfg.self),
		announced:    make(map[string]uint32, 32),
		nextSync:     now.Add(a.cfg.syncCheckInterval),
		nextPresence: now,
	}
	existing, err := messagesIn(w.inbox)
	if err != nil {
		return nil, err
	}
	for _, name := range existing {
		w.announced[name] = w.scan
	}
	return w, nil
}

func (w *watcher) poll(now time.Time) error {
	if err := w.refreshDeadline(); err != nil {
		return err
	}
	if w.hasDeadline {
		if err := deadlinePassed(w.deadline, now); err != nil {
			return err
		}
	}
	if !isFile(w.memberFile) {
		return errors.New("'" + w.agent.cfg.self + "' is no longer a member of this mailbox")
	}
	if !now.Before(w.nextSync) {
		if err := w.agent.checkSync(); err != nil {
			return err
		}
		w.nextSync = now.Add(w.agent.cfg.syncCheckInterval)
	}
	if err := w.announceArrivals(now); err != nil {
		return err
	}
	if !now.Before(w.nextPresence) {
		if err := w.agent.recordPresence(); err != nil {
			return err
		}
		w.nextPresence = now.Add(presenceRefresh)
	}
	return w.agent.out.err
}

func (w *watcher) refreshDeadline() error {
	info, err := os.Stat(w.deadlineFile)
	if errors.Is(err, fs.ErrNotExist) {
		w.hasDeadline = false
		return nil
	}
	if err != nil {
		return deadError{"cannot read " + w.deadlineFile + ": " + err.Error()}
	}
	if w.hasDeadline && info.ModTime().Equal(w.deadlineMod) {
		return nil
	}
	deadline, present, err := w.agent.readDeadline()
	if err != nil {
		return err
	}
	w.deadline, w.hasDeadline, w.deadlineMod = deadline, present, info.ModTime()
	return nil
}

func (w *watcher) announceArrivals(now time.Time) error {
	info, err := os.Stat(w.inbox)
	if err != nil {
		return deadError{"the inbox " + w.inbox + " is gone"}
	}
	if info.ModTime().Equal(w.inboxChanged) && now.Before(w.nextRescan) {
		return nil
	}
	entries, err := os.ReadDir(w.inbox)
	if err != nil {
		return deadError{"cannot list the inbox " + w.inbox + ": " + err.Error()}
	}
	w.scan++
	for _, entry := range entries {
		if !isMessage(entry) {
			continue
		}
		name := entry.Name()
		if _, known := w.announced[name]; !known {
			header, _ := readMessageHeader(filepath.Join(w.inbox, name))
			w.agent.out.add("NEW MAIL: ", printable(name), " from ", senderOf(header)).end()
		}
		w.announced[name] = w.scan
	}
	for name, scan := range w.announced {
		if scan != w.scan {
			delete(w.announced, name)
		}
	}
	w.inboxChanged = info.ModTime()
	w.nextRescan = now.Add(fullRescanEvery)
	return nil
}
