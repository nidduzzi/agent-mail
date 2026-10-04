package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

var version = "dev"

const (
	exitOK      = 0
	exitRefused = 1
	exitDead    = 2
)

const usage = `usage: agent-mail <command>
  version                                  print the version
  config                                   resolved settings
  init [--deadline 'YYYY-MM-DD HH:MM']     create the mailbox at AGENT_MAIL_DIR (deadline in UTC)
  check                                    deadline first; exit 2 with DEAD: when closed
  join <name> [role]                       claim a name and an inbox
  who                                      members, last seen, unread counts
  leave                                    drop this agent's membership
  list [show [list] | set <list> <member>... | delete <list>]
  send [--reply-to <id>] <name,name,@list> <slug> < body.md
  inbox                                    unread mail, and own mail not yet read
  ack <file>                               mark a message read
  watch                                    print NEW MAIL lines until the mailbox dies`

type deadError struct{ reason string }

func (e deadError) Error() string { return "DEAD: " + e.reason }

type agent struct {
	cfg    config
	box    mailbox
	now    func() time.Time
	host   string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return exitRefused
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitRefused
	}
	host, _ := os.Hostname()
	a := agent{
		cfg:    cfg,
		box:    mailbox{dir: cfg.dir},
		now:    time.Now,
		host:   host,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	err = a.dispatch(args[0], args[1:])
	var dead deadError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &dead):
		fmt.Fprintln(stderr, dead.Error())
		return exitDead
	default:
		fmt.Fprintln(stderr, err)
		return exitRefused
	}
}

func (a agent) dispatch(command string, args []string) error {
	switch command {
	case "version":
		fmt.Fprintln(a.stdout, version)
		return nil
	case "config":
		a.cfg.print(a.stdout)
		return nil
	case "init":
		return a.createMailbox(args)
	case "check":
		return a.checkLive(a.stdout)
	case "join":
		return a.whenLive(io.Discard, func() error { return a.join(args) })
	case "who":
		return a.whenLive(a.stdout, a.who)
	case "leave":
		return a.asMember(io.Discard, a.leave)
	case "list":
		return a.whenLive(io.Discard, func() error { return a.manageLists(args) })
	case "send":
		return a.asMember(a.stdout, func() error { return a.send(args) })
	case "inbox":
		return a.asMember(a.stdout, a.inbox)
	case "ack":
		return a.asMember(io.Discard, func() error { return a.acknowledge(args) })
	case "watch":
		return a.asMember(io.Discard, a.watch)
	default:
		return errors.New(usage)
	}
}

func (a agent) whenLive(report io.Writer, action func() error) error {
	if err := a.checkLive(report); err != nil {
		return err
	}
	return action()
}

func (a agent) asMember(report io.Writer, action func() error) error {
	return a.whenLive(report, func() error {
		if err := a.requireIdentity(); err != nil {
			return err
		}
		return action()
	})
}
