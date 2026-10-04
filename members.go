package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const presenceRefresh = 5 * time.Minute

type member struct {
	name   string
	role   string
	host   string
	joined string
	seen   time.Time
}

func readMember(box mailbox, name string) (member, error) {
	raw, err := os.ReadFile(box.memberFile(name))
	if err != nil {
		return member{}, err
	}
	fields := headerFields(string(raw))
	seconds, _ := strconv.ParseInt(fields["seen"], 10, 64)
	return member{
		name:   name,
		role:   fields["role"],
		host:   fields["host"],
		joined: fields["joined"],
		seen:   time.Unix(seconds, 0),
	}, nil
}

func (m member) render() string {
	return "role: " + m.role + "\nhost: " + m.host + "\njoined: " + m.joined + "\nseen: " + strconv.FormatInt(m.seen.Unix(), 10) + "\n"
}

func (m member) write(box mailbox) error {
	return replaceFileAtomically(box.memberFile(m.name), m.render())
}

func headerFields(text string) map[string]string {
	fields := make(map[string]string, 6)
	for len(text) > 0 {
		line, rest, _ := strings.Cut(text, "\n")
		text = rest
		key, value, found := strings.Cut(line, ": ")
		if _, seen := fields[key]; found && !seen {
			fields[key] = value
		}
	}
	return fields
}

func (a agent) join(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: agent-mail join <name> [role]")
	}
	joining := member{name: args[0], host: printable(a.host), joined: a.now().UTC().Format(time.RFC3339), seen: a.now()}
	if len(args) == 2 {
		joining.role = args[1]
	}
	if !validName(joining.name) {
		return errors.New("a name is 1 to 64 lowercase letters, digits and dashes")
	}
	if !validLine(joining.role) {
		return errors.New("a role is one line of at most 200 printable characters")
	}
	if existing, err := readMember(a.box, joining.name); err == nil {
		idle := a.now().Sub(existing.seen)
		rejoining := a.cfg.self == joining.name
		switch {
		case rejoining:
		case idle < a.cfg.staleAfter:
			return errors.New("'" + joining.name + "' is taken: seen " + strconv.Itoa(int(idle.Minutes())) + " min ago on " + printable(existing.host))
		default:
			a.out.add("taking over '", joining.name, "', idle for ").num(int64(idle.Minutes())).add(" min").end()
		}
		joining.joined = existing.joined
	}
	if err := os.MkdirAll(a.box.readDir(joining.name), 0o755); err != nil {
		return err
	}
	if err := joining.write(a.box); err != nil {
		return err
	}
	a.out.add("joined as '", joining.name, "'; this agent now needs AGENT_MAIL_SELF=", joining.name).end()
	return nil
}

func (a agent) leave() error {
	if err := os.Remove(a.box.memberFile(a.cfg.self)); err != nil {
		return err
	}
	a.out.add("'", a.cfg.self, "' left; its inbox to-", a.cfg.self, "/ is kept").end()
	return nil
}

func (a agent) who() error {
	entries, err := os.ReadDir(a.box.membersDir())
	if err != nil {
		return err
	}
	a.out.cell("NAME", 21).cell("ROLE", 21).cell("HOST", 21).cell("LAST-SEEN", 13).add("UNREAD").end()
	now := a.now()
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !validName(entry.Name()) {
			continue
		}
		m, err := readMember(a.box, entry.Name())
		if err != nil {
			return err
		}
		unread, err := countMessages(a.box.inbox(m.name))
		if err != nil {
			unread = 0
		}
		idle := now.Sub(m.seen)
		lastSeen := strconv.Itoa(int(idle.Minutes())) + "m ago"
		if idle >= a.cfg.staleAfter {
			lastSeen = "stale " + strconv.Itoa(int(idle.Minutes())) + "m"
		}
		a.out.cell(m.name, 21).cell(printable(m.role), 21).cell(printable(m.host), 21).cell(lastSeen, 13).num(int64(unread)).end()
	}
	return nil
}

func (a agent) recordPresence() error {
	m, err := readMember(a.box, a.cfg.self)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("'" + a.cfg.self + "' is no longer a member of this mailbox")
	}
	if err != nil {
		return err
	}
	if a.now().Sub(m.seen) < presenceRefresh {
		return nil
	}
	m.seen = a.now()
	return m.write(a.box)
}
