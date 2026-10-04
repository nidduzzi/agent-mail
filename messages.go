package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var secretPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key)\s*[=:]\s*[^\s<]{6,}|BEGIN [A-Z ]*PRIVATE KEY`)

type message struct {
	id        string
	from      string
	to        []string
	inReplyTo string
	body      string
}

func (m message) render() string {
	var header strings.Builder
	fmt.Fprintf(&header, "---\nid: %s\nfrom: %s\nto: %s\n", m.id, m.from, strings.Join(m.to, ", "))
	if m.inReplyTo != "" {
		fmt.Fprintf(&header, "in-reply-to: %s\n", m.inReplyTo)
	}
	return header.String() + "---\n" + m.body + "\n"
}

func readMessageHeader(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "---\n")
	header, _, _ := strings.Cut(text, "\n---\n")
	return headerFields(header), nil
}

func messagesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func (a agent) resolveRecipients(addressed string) ([]string, error) {
	var resolved, unknown []string
	for _, item := range strings.Split(addressed, ",") {
		names := []string{item}
		if list, isList := strings.CutPrefix(item, "@"); isList {
			members, err := readList(a.box, list)
			if err != nil {
				unknown = append(unknown, item)
				continue
			}
			names = members
		}
		for _, name := range names {
			switch {
			case name == a.cfg.self || slices.Contains(resolved, name):
			case !isDir(a.box.inbox(name)):
				unknown = append(unknown, name)
			default:
				resolved = append(resolved, name)
			}
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown recipients: %s (see agent-mail who and agent-mail list)", strings.Join(unknown, " "))
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no recipients besides '%s'", a.cfg.self)
	}
	return resolved, nil
}

func (a agent) send(args []string) error {
	const usageLine = "usage: agent-mail send [--reply-to <id>] <name,name,@list> <slug> < body.md"
	var inReplyTo string
	if len(args) >= 2 && args[0] == "--reply-to" {
		inReplyTo, args = args[1], args[2:]
	}
	if len(args) != 2 {
		return errors.New(usageLine)
	}
	addressed, slug := args[0], args[1]
	if !namePattern.MatchString(slug) {
		return errors.New("a slug is lowercase letters, digits and dashes")
	}
	recipients, err := a.resolveRecipients(addressed)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(a.stdin)
	if err != nil {
		return err
	}
	body := strings.TrimRight(string(raw), "\n")
	if strings.TrimSpace(body) == "" {
		return errors.New("empty message")
	}
	if secretPattern.MatchString(body) {
		return errors.New("refused: the body looks like it carries a secret")
	}

	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	outgoing := message{
		id:        fmt.Sprintf("%s-%s-from-%s-%s", a.now().UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix), a.cfg.self, slug),
		from:      a.cfg.self,
		to:        recipients,
		inReplyTo: inReplyTo,
		body:      body,
	}
	for _, recipient := range recipients {
		if err := writeFileAtomically(filepath.Join(a.box.inbox(recipient), outgoing.id+".md"), outgoing.render()); err != nil {
			return err
		}
	}
	if err := a.recordPresence(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "sent %s to: %s\n", outgoing.id, strings.Join(recipients, " "))
	return nil
}

func (a agent) inbox() error {
	unread, err := messagesIn(a.box.inbox(a.cfg.self))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "unread in to-%s:\n", a.cfg.self)
	for _, name := range unread {
		header, err := readMessageHeader(filepath.Join(a.box.inbox(a.cfg.self), name))
		if err != nil {
			return err
		}
		line := fmt.Sprintf("  %s  from %s", name, header["from"])
		if header["in-reply-to"] != "" {
			line += "  re " + header["in-reply-to"]
		}
		fmt.Fprintln(a.stdout, line)
	}

	fmt.Fprintf(a.stdout, "sent by %s, not yet read:\n", a.cfg.self)
	inboxes, err := filepath.Glob(filepath.Join(a.box.dir, "to-*"))
	if err != nil {
		return err
	}
	for _, inbox := range inboxes {
		pending, err := messagesIn(inbox)
		if err != nil {
			continue
		}
		for _, name := range pending {
			header, err := readMessageHeader(filepath.Join(inbox, name))
			if err == nil && header["from"] == a.cfg.self {
				fmt.Fprintf(a.stdout, "  %s/%s\n", filepath.Base(inbox), name)
			}
		}
	}
	return a.recordPresence()
}

func (a agent) acknowledge(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agent-mail ack <file in the inbox>")
	}
	name := filepath.Base(args[0])
	if err := os.Rename(filepath.Join(a.box.inbox(a.cfg.self), name), filepath.Join(a.box.readDir(a.cfg.self), name)); err != nil {
		return fmt.Errorf("no unread message %s in to-%s", name, a.cfg.self)
	}
	if err := a.recordPresence(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "acked: %s\n", name)
	return nil
}

func (a agent) watch() error {
	announced := map[string]bool{}
	existing, err := messagesIn(a.box.inbox(a.cfg.self))
	if err != nil {
		return err
	}
	for _, name := range existing {
		announced[name] = true
	}
	for {
		if err := a.checkLive(io.Discard); err != nil {
			return err
		}
		arrived, err := messagesIn(a.box.inbox(a.cfg.self))
		if err != nil {
			return err
		}
		for _, name := range arrived {
			if announced[name] {
				continue
			}
			announced[name] = true
			header, _ := readMessageHeader(filepath.Join(a.box.inbox(a.cfg.self), name))
			fmt.Fprintf(a.stdout, "NEW MAIL: %s from %s\n", name, header["from"])
		}
		if err := a.recordPresence(); err != nil {
			return err
		}
		time.Sleep(a.cfg.pollInterval)
	}
}
