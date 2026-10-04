package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const deadlineLayout = "2006-01-02 15:04"

type mailbox struct{ dir string }

func (m mailbox) membersDir() string            { return filepath.Join(m.dir, "members") }
func (m mailbox) listsDir() string              { return filepath.Join(m.dir, "lists") }
func (m mailbox) deadlineFile() string          { return filepath.Join(m.dir, "deadline") }
func (m mailbox) memberFile(name string) string { return filepath.Join(m.membersDir(), name) }
func (m mailbox) listFile(name string) string   { return filepath.Join(m.listsDir(), name) }
func (m mailbox) inbox(name string) string      { return filepath.Join(m.dir, "to-"+name) }
func (m mailbox) readDir(name string) string    { return filepath.Join(m.inbox(name), "read") }

func validName(name string) bool {
	if name == "" || name[0] == '-' || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validMessageID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validLine(text string) bool {
	return len(text) <= 200 && printable(text) == text
}

func temporaryPathFor(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
}

func replaceFileAtomically(path, content string) error {
	partial := temporaryPathFor(path)
	if err := os.WriteFile(partial, []byte(content), 0o644); err != nil {
		os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		os.Remove(partial)
		return err
	}
	return nil
}

func createFileAtomically(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return errors.New(path + " already exists")
	}
	return replaceFileAtomically(path, content)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func parseDeadline(text string) (time.Time, error) {
	return time.ParseInLocation(deadlineLayout, strings.TrimSpace(text), time.UTC)
}

func (a agent) readDeadline() (deadline time.Time, present bool, err error) {
	raw, err := os.ReadFile(a.box.deadlineFile())
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, deadError{"cannot read " + a.box.deadlineFile() + ": " + err.Error()}
	}
	text, _, _ := strings.Cut(string(raw), "\n")
	deadline, err = parseDeadline(text)
	if err != nil {
		return time.Time{}, false, deadError{a.box.deadlineFile() + " holds '" + printable(text) + "', not a UTC 'YYYY-MM-DD HH:MM'"}
	}
	return deadline, true, nil
}

func deadlinePassed(deadline, now time.Time) error {
	if now.Before(deadline) {
		return nil
	}
	return deadError{"the deadline passed " + strconv.FormatInt(int64(now.Sub(deadline).Minutes()), 10) + " min ago; the mailbox is closed"}
}

func (a agent) createMailbox(args []string) error {
	var deadlineText string
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--deadline":
		if _, err := parseDeadline(args[1]); err != nil {
			return errors.New("the deadline must be a UTC 'YYYY-MM-DD HH:MM'")
		}
		deadlineText = args[1]
	default:
		return errors.New("usage: agent-mail init [--deadline 'YYYY-MM-DD HH:MM'] (UTC)")
	}
	if isDir(a.box.membersDir()) {
		return errors.New("a mailbox already exists at " + a.box.dir)
	}
	for _, dir := range [...]string{a.box.membersDir(), a.box.listsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	a.out.add("created mailbox at ", a.box.dir)
	if deadlineText != "" {
		if err := replaceFileAtomically(a.box.deadlineFile(), deadlineText+"\n"); err != nil {
			return err
		}
		a.out.add(", deadline ", deadlineText, " UTC")
	}
	a.out.end()
	return nil
}

func (a agent) checkLive(report *output) error {
	if !isDir(a.box.membersDir()) {
		return deadError{"no mailbox at " + a.box.dir + " (create one with: agent-mail init)"}
	}
	deadline, present, err := a.readDeadline()
	if err != nil {
		return err
	}
	if present {
		report.add("AGENT-MAIL DEADLINE: ", deadline.Format(deadlineLayout), " UTC (",
			deadline.Local().Format("Mon 2006-01-02 15:04 MST"), ")").end()
		now := a.now()
		if err := deadlinePassed(deadline, now); err != nil {
			return err
		}
		left := deadline.Sub(now)
		report.add("time left: ").num(int64(left.Hours())).add("h ").twoDigits(int64(left.Minutes()) % 60).add("m").end()
	} else {
		report.add("AGENT-MAIL DEADLINE: none").end()
	}
	return a.checkSync()
}

func (a agent) checkSync() error {
	if a.cfg.syncCheck == "" || runSucceeds(a.cfg.syncCheck) {
		return nil
	}
	return deadError{"the sync check failed: " + a.cfg.syncCheck}
}

func runSucceeds(command string) bool {
	shell, argv := "/bin/sh", []string{"sh", "-c", command}
	if runtime.GOOS == "windows" {
		shell = os.Getenv("ComSpec")
		if shell == "" {
			shell = `C:\Windows\System32\cmd.exe`
		}
		argv = []string{"cmd", "/C", command}
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer devNull.Close()
	process, err := os.StartProcess(shell, argv, &os.ProcAttr{Files: []*os.File{devNull, devNull, devNull}})
	if err != nil {
		return false
	}
	state, err := process.Wait()
	return err == nil && state.Success()
}

func (a agent) requireIdentity() error {
	if a.cfg.self == "" {
		return errors.New("no identity: set AGENT_MAIL_SELF for this agent (join first: agent-mail join <name>)")
	}
	if !validName(a.cfg.self) {
		return errors.New("AGENT_MAIL_SELF '" + printable(a.cfg.self) + "' is not a valid name: lowercase letters, digits and dashes")
	}
	if !isFile(a.box.memberFile(a.cfg.self)) || !isDir(a.box.readDir(a.cfg.self)) {
		return errors.New("'" + a.cfg.self + "' is not a member of this mailbox (agent-mail join " + a.cfg.self + ")")
	}
	return nil
}
