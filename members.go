package main

import (
	"errors"
	"fmt"
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
	return fmt.Sprintf("role: %s\nhost: %s\njoined: %s\nseen: %d\n", m.role, m.host, m.joined, m.seen.Unix())
}

func headerFields(text string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, found := strings.Cut(line, ": ")
		if found {
			if _, seen := fields[key]; !seen {
				fields[key] = value
			}
		}
	}
	return fields
}

func (a agent) join(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: agent-mail join <name> [role]")
	}
	name, role := args[0], ""
	if len(args) == 2 {
		role = args[1]
	}
	if !namePattern.MatchString(name) {
		return errors.New("a name is lowercase letters, digits and dashes")
	}
	joined := a.now().UTC().Format(time.RFC3339)
	if existing, err := readMember(a.box, name); err == nil {
		idle := a.now().Sub(existing.seen)
		if idle < a.cfg.staleAfter {
			return fmt.Errorf("'%s' is taken: seen %d min ago on %s", name, int(idle.Minutes()), existing.host)
		}
		fmt.Fprintf(a.stdout, "taking over '%s', idle for %d min\n", name, int(idle.Minutes()))
		joined = existing.joined
	}
	if err := os.MkdirAll(a.box.readDir(name), 0o755); err != nil {
		return err
	}
	joining := member{name: name, role: role, host: a.host, joined: joined, seen: a.now()}
	if err := writeFileAtomically(a.box.memberFile(name), joining.render()); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "joined as '%s'; this agent now needs AGENT_MAIL_SELF=%s\n", name, name)
	return nil
}

func (a agent) leave() error {
	if err := os.Remove(a.box.memberFile(a.cfg.self)); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "'%s' left; its inbox to-%s/ is kept\n", a.cfg.self, a.cfg.self)
	return nil
}

func (a agent) who() error {
	entries, err := os.ReadDir(a.box.membersDir())
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%-20s %-20s %-20s %-12s %s\n", "NAME", "ROLE", "HOST", "LAST-SEEN", "UNREAD")
	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		m, err := readMember(a.box, entry.Name())
		if err != nil {
			return err
		}
		idle := int(a.now().Sub(m.seen).Minutes())
		lastSeen := fmt.Sprintf("%dm ago", idle)
		if a.now().Sub(m.seen) >= a.cfg.staleAfter {
			lastSeen = fmt.Sprintf("stale %dm", idle)
		}
		unread, err := messagesIn(a.box.inbox(m.name))
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%-20s %-20s %-20s %-12s %d\n", m.name, m.role, m.host, lastSeen, len(unread))
	}
	return nil
}

func (a agent) recordPresence() error {
	m, err := readMember(a.box, a.cfg.self)
	if err != nil {
		return err
	}
	if a.now().Sub(m.seen) < presenceRefresh {
		return nil
	}
	m.seen = a.now()
	return writeFileAtomically(a.box.memberFile(m.name), m.render())
}
