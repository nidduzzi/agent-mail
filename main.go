package main

import (
	"errors"
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
  watch                                    print NEW MAIL lines until the mailbox dies
  doctor                                   what is missing, and how to fix it (changes nothing)
  sync id | status                         this device's Syncthing ID; how the mailbox is shared
  sync share <device-id> [--address tcp://ip:22000|dynamic] [--name n] [--yes]`

type deadError struct{ reason string }

func (e deadError) Error() string { return "DEAD: " + e.reason }

type agent struct {
	cfg   config
	box   mailbox
	now   func() time.Time
	host  string
	stdin io.Reader
	out   *output
	quiet *output
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	errOut := newOutput(stderr)
	if len(args) == 0 {
		errOut.add(usage).end()
		return exitRefused
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		errOut.add(err.Error()).end()
		return exitRefused
	}
	host, _ := os.Hostname()
	a := agent{
		cfg:   cfg,
		box:   mailbox{dir: cfg.dir},
		now:   time.Now,
		host:  host,
		stdin: stdin,
		out:   newOutput(stdout),
		quiet: newOutput(io.Discard),
	}
	err = a.dispatch(args[0], args[1:])
	if err == nil {
		err = a.out.err
	}
	var dead deadError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &dead):
		errOut.add(dead.Error()).end()
		return exitDead
	default:
		errOut.add(err.Error()).end()
		return exitRefused
	}
}

func (a agent) dispatch(command string, args []string) error {
	switch command {
	case "version":
		a.out.add(version).end()
		return nil
	case "config":
		a.cfg.print(a.out)
		return nil
	case "init":
		return a.createMailbox(args)
	case "check":
		return a.checkLive(a.out)
	case "join":
		return a.whenLive(a.quiet, func() error { return a.join(args) })
	case "who":
		return a.whenLive(a.out, a.who)
	case "leave":
		return a.asMember(a.quiet, a.leave)
	case "list":
		return a.whenLive(a.quiet, func() error { return a.manageLists(args) })
	case "send":
		return a.asMember(a.out, func() error { return a.send(args) })
	case "inbox":
		return a.asMember(a.out, a.inbox)
	case "ack":
		return a.asMember(a.quiet, func() error { return a.acknowledge(args) })
	case "watch":
		return a.asMember(a.quiet, a.watch)
	case "doctor":
		return a.doctor()
	case "sync":
		return a.syncCommand(args)
	default:
		return errors.New(usage)
	}
}

func (a agent) whenLive(report *output, action func() error) error {
	if err := a.checkLive(report); err != nil {
		return err
	}
	return action()
}

func (a agent) asMember(report *output, action func() error) error {
	return a.whenLive(report, func() error {
		if err := a.requireIdentity(); err != nil {
			return err
		}
		return action()
	})
}
