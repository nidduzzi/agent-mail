package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const maxBodyBytes = 1 << 20

var secretKeywords = [...]string{
	"password", "passwd", "secret", "token",
	"apikey", "api_key", "api-key",
	"privatekey", "private_key", "private-key",
}

type message struct {
	id        string
	from      string
	to        []string
	inReplyTo string
	body      string
}

func (m message) render() string {
	var text strings.Builder
	text.Grow(96 + len(m.id) + len(m.inReplyTo) + len(m.body) + 16*len(m.to))
	text.WriteString("---\nid: ")
	text.WriteString(m.id)
	text.WriteString("\nfrom: ")
	text.WriteString(m.from)
	text.WriteString("\nto: ")
	text.WriteString(strings.Join(m.to, ", "))
	if m.inReplyTo != "" {
		text.WriteString("\nin-reply-to: ")
		text.WriteString(m.inReplyTo)
	}
	text.WriteString("\n---\n")
	text.WriteString(m.body)
	text.WriteString("\n")
	return text.String()
}

func readMessageHeader(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text, opened := strings.CutPrefix(string(raw), "---\n")
	header, _, closed := strings.Cut(text, "\n---\n")
	if !opened || !closed {
		return map[string]string{}, nil
	}
	return headerFields(header), nil
}

func senderOf(header map[string]string) string {
	if from := header["from"]; validName(from) {
		return from
	}
	return "(unknown sender)"
}

func isMessage(entry os.DirEntry) bool {
	return entry.Type().IsRegular() && isMessageName(entry.Name())
}

func isMessageName(name string) bool {
	return strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".")
}

func messagesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if isMessage(entry) {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func countMessages(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if isMessage(entry) {
			count++
		}
	}
	return count, nil
}

func looksLikeSecret(body string) bool {
	if strings.Contains(body, "BEGIN ") && strings.Contains(body, "PRIVATE KEY") {
		return true
	}
	lower := strings.ToLower(body)
	for _, keyword := range secretKeywords {
		for searchFrom := 0; ; {
			at := strings.Index(lower[searchFrom:], keyword)
			if at < 0 {
				break
			}
			afterKeyword := strings.TrimLeft(lower[searchFrom+at+len(keyword):], " \t\r\n")
			if afterKeyword != "" && (afterKeyword[0] == '=' || afterKeyword[0] == ':') {
				value := strings.TrimLeft(afterKeyword[1:], " \t\r\n")
				length := 0
				for length < len(value) && !strings.ContainsRune(" \t\r\n<", rune(value[length])) {
					length++
				}
				if length >= 6 {
					return true
				}
			}
			searchFrom += at + 1
		}
	}
	return false
}

func (a agent) resolveRecipients(addressed string) ([]string, error) {
	items := strings.Split(addressed, ",")
	resolved := make([]string, 0, len(items))
	var unknown []string
	for _, item := range items {
		names := []string{item}
		if list, isList := strings.CutPrefix(item, "@"); isList {
			members, err := readList(a.box, list)
			if err != nil {
				unknown = append(unknown, printable(item))
				continue
			}
			names = members
		}
		for _, name := range names {
			switch {
			case !validName(name) || !isFile(a.box.memberFile(name)):
				unknown = append(unknown, printable(name))
			case name == a.cfg.self || slices.Contains(resolved, name):
			default:
				resolved = append(resolved, name)
			}
		}
	}
	if len(unknown) > 0 {
		return nil, errors.New("unknown recipients: " + strings.Join(unknown, " ") + " (see agent-mail who and agent-mail list)")
	}
	if len(resolved) == 0 {
		return nil, errors.New("no recipients besides '" + a.cfg.self + "'")
	}
	return resolved, nil
}

func (a agent) newMessageID(slug string) string {
	now := a.now().UTC()
	suffix := uint64(now.UnixNano())&0xffffff ^ uint64(os.Getpid())&0xffffff
	hex := strconv.FormatUint(suffix|1<<24, 16)[1:]
	return now.Format("20060102T150405Z") + "-" + hex + "-from-" + a.cfg.self + "-" + slug
}

func (a agent) send(args []string) error {
	const usageLine = "usage: agent-mail send [--reply-to <id>] <name,name,@list> <slug> < body.md"
	var inReplyTo string
	if len(args) >= 2 && args[0] == "--reply-to" {
		inReplyTo, args = args[1], args[2:]
		if !validMessageID(inReplyTo) {
			return errors.New("--reply-to takes a message id: letters, digits and dashes")
		}
	}
	if len(args) != 2 {
		return errors.New(usageLine)
	}
	addressed, slug := args[0], args[1]
	if !validName(slug) {
		return errors.New("a slug is 1 to 64 lowercase letters, digits and dashes")
	}
	recipients, err := a.resolveRecipients(addressed)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(a.stdin, maxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBodyBytes {
		return errors.New("refused: the body is over " + strconv.Itoa(maxBodyBytes) + " bytes")
	}
	body := strings.TrimRight(string(raw), "\n")
	if strings.TrimSpace(body) == "" {
		return errors.New("empty message")
	}
	if looksLikeSecret(body) {
		return errors.New("refused: the body looks like it carries a secret")
	}

	outgoing := message{id: a.newMessageID(slug), from: a.cfg.self, to: recipients, inReplyTo: inReplyTo, body: body}
	rendered := outgoing.render()
	delivered := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		if err := createFileAtomically(filepath.Join(a.box.inbox(recipient), outgoing.id+".md"), rendered); err != nil {
			return errors.New("delivered " + outgoing.id + " to: " + strings.Join(delivered, " ") + "; failed for " + recipient + ": " + err.Error())
		}
		delivered = append(delivered, recipient)
	}
	if err := a.recordPresence(); err != nil {
		return err
	}
	a.out.add("sent ", outgoing.id, " to: ", strings.Join(recipients, " ")).end()
	return nil
}

func (a agent) inbox() error {
	own := a.box.inbox(a.cfg.self)
	unread, err := messagesIn(own)
	if err != nil {
		return err
	}
	a.out.add("unread in to-", a.cfg.self, ":").end()
	for _, name := range unread {
		header, err := readMessageHeader(filepath.Join(own, name))
		if err != nil {
			return err
		}
		a.out.add("  ", printable(name), "  from ", senderOf(header))
		if reply := header["in-reply-to"]; validMessageID(reply) {
			a.out.add("  re ", reply)
		}
		a.out.end()
	}

	a.out.add("sent by ", a.cfg.self, ", not yet read:").end()
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
				a.out.add("  ", printable(filepath.Base(inbox)), "/", printable(name)).end()
			}
		}
	}
	return a.recordPresence()
}

func (a agent) acknowledge(args []string) error {
	if len(args) != 1 || !isMessageName(filepath.Base(args[0])) {
		return errors.New("usage: agent-mail ack <message file in the inbox, *.md>")
	}
	name := filepath.Base(args[0])
	if err := os.Rename(filepath.Join(a.box.inbox(a.cfg.self), name), filepath.Join(a.box.readDir(a.cfg.self), name)); err != nil {
		return errors.New("no unread message " + printable(name) + " in to-" + a.cfg.self)
	}
	if err := a.recordPresence(); err != nil {
		return err
	}
	a.out.add("acked: ", name).end()
	return nil
}
