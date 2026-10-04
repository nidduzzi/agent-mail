package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const deadlineLayout = "2006-01-02 15:04"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type mailbox struct{ dir string }

func (m mailbox) membersDir() string            { return filepath.Join(m.dir, "members") }
func (m mailbox) listsDir() string              { return filepath.Join(m.dir, "lists") }
func (m mailbox) deadlineFile() string          { return filepath.Join(m.dir, "deadline") }
func (m mailbox) memberFile(name string) string { return filepath.Join(m.membersDir(), name) }
func (m mailbox) listFile(name string) string   { return filepath.Join(m.listsDir(), name) }
func (m mailbox) inbox(name string) string      { return filepath.Join(m.dir, "to-"+name) }
func (m mailbox) readDir(name string) string    { return filepath.Join(m.inbox(name), "read") }

func writeFileAtomically(path, content string) error {
	partial := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(partial, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(partial, path)
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
		return fmt.Errorf("a mailbox already exists at %s", a.box.dir)
	}
	for _, dir := range []string{a.box.membersDir(), a.box.listsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if deadlineText != "" {
		if err := writeFileAtomically(a.box.deadlineFile(), deadlineText+"\n"); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "created mailbox at %s, deadline %s UTC\n", a.box.dir, deadlineText)
		return nil
	}
	fmt.Fprintf(a.stdout, "created mailbox at %s\n", a.box.dir)
	return nil
}

func (a agent) checkLive(report io.Writer) error {
	if !isDir(a.box.membersDir()) {
		return deadError{fmt.Sprintf("no mailbox at %s (create one with: agent-mail init)", a.box.dir)}
	}

	raw, err := os.ReadFile(a.box.deadlineFile())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(report, "AGENT-MAIL DEADLINE: none")
	case err != nil:
		return deadError{fmt.Sprintf("cannot read %s: %v", a.box.deadlineFile(), err)}
	default:
		text := strings.SplitN(string(raw), "\n", 2)[0]
		deadline, err := parseDeadline(text)
		if err != nil {
			return deadError{fmt.Sprintf("%s holds '%s', not a UTC 'YYYY-MM-DD HH:MM'", a.box.deadlineFile(), text)}
		}
		fmt.Fprintf(report, "AGENT-MAIL DEADLINE: %s UTC (%s)\n",
			deadline.Format(deadlineLayout), deadline.Local().Format("Mon 2006-01-02 15:04 MST"))
		left := deadline.Sub(a.now())
		if left <= 0 {
			return deadError{fmt.Sprintf("the deadline passed %d min ago; the mailbox is closed", int(-left.Minutes()))}
		}
		fmt.Fprintf(report, "time left: %dh %02dm\n", int(left.Hours()), int(left.Minutes())%60)
	}

	if a.cfg.syncCheck != "" {
		if err := shellCommand(a.cfg.syncCheck).Run(); err != nil {
			return deadError{"the sync check failed: " + a.cfg.syncCheck}
		}
	}
	return nil
}

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

func (a agent) requireIdentity() error {
	if a.cfg.self == "" {
		return errors.New("no identity: set AGENT_MAIL_SELF for this agent (join first: agent-mail join <name>)")
	}
	if !isFile(a.box.memberFile(a.cfg.self)) || !isDir(a.box.readDir(a.cfg.self)) {
		return fmt.Errorf("'%s' has not joined this mailbox (agent-mail join %s)", a.cfg.self, a.cfg.self)
	}
	return nil
}
