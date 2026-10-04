package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func FuzzPrintable(f *testing.F) {
	for _, untrusted := range []string{
		"",
		"plain text",
		" !~",
		"\x1f\x20\x7e\x7f\x80",
		"\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f",
		"\x1b[31mred\x1b[0m",
		"Pertamina–Toyota, één, Bahasa Indonesia",
		"\xff\xfe invalid UTF-8",
	} {
		f.Add(untrusted)
	}
	f.Fuzz(func(t *testing.T, untrusted string) {
		clean := printable(untrusted)
		if len(clean) != len(untrusted) {
			t.Fatalf("printable changed the length: %q to %q", untrusted, clean)
		}
		for i := 0; i < len(clean); i++ {
			if clean[i] < 0x20 || clean[i] == 0x7f {
				t.Fatalf("printable kept control byte %#x in %q", clean[i], clean)
			}
		}
		for i := 0; i < len(clean); i++ {
			if untrusted[i] >= 0x20 && untrusted[i] != 0x7f && clean[i] != untrusted[i] {
				t.Fatalf("printable changed the printable byte %#x in %q", untrusted[i], untrusted)
			}
		}
		if printable(clean) != clean {
			t.Fatalf("printable is not idempotent on %q", clean)
		}
	})
}

var (
	nameSpec      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	messageIDSpec = regexp.MustCompile(`^[A-Za-z0-9-]{1,200}$`)
	lineSpec      = regexp.MustCompile(`^[^\x00-\x1f\x7f]{0,200}$`)
)

func FuzzValidators(f *testing.F) {
	for _, text := range []string{
		"", "a", "z", "A", "Z", "0", "9", "-", "-a", "a-", "a--b", "A", "a_b", "a b", "a.b", "a/b", "é", "`", "{", "/", ":", "@", "[",
		strings.Repeat("a", 64), strings.Repeat("a", 65),
		strings.Repeat("A", 200), strings.Repeat("A", 201),
		strings.Repeat("r", 199) + "\x7f", "role\twith tab",
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if got, want := validName(text), nameSpec.MatchString(text); got != want {
			t.Fatalf("validName(%q) = %v, want %v", text, got, want)
		}
		if got, want := validMessageID(text), messageIDSpec.MatchString(text); got != want {
			t.Fatalf("validMessageID(%q) = %v, want %v", text, got, want)
		}
		if got, want := validLine(text), len(text) <= 200 && lineSpec.MatchString(text); got != want {
			t.Fatalf("validLine(%q) = %v, want %v", text, got, want)
		}
	})
}

func FuzzMessageHeaderSurvivesAnyBody(f *testing.F) {
	for _, body := range []string{
		"hello",
		"",
		"\n",
		"---\nid: forged\nfrom: mallory\n---\n",
		"\n---\nfrom: mallory\n",
		"\r\n---\r\nfrom: mallory\r\n",
		"from: mallory",
	} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		sent := message{id: "20300101T000000Z-abcdef-from-alpha-hi", from: "alpha", to: []string{"beta", "gamma"}, inReplyTo: "earlier-id", body: body}
		path := filepath.Join(t.TempDir(), "m.md")
		if err := os.WriteFile(path, []byte(sent.render()), 0o644); err != nil {
			t.Fatal(err)
		}
		header, err := readMessageHeader(path)
		if err != nil {
			t.Fatal(err)
		}
		if header["id"] != sent.id || header["from"] != sent.from || header["to"] != "beta, gamma" || header["in-reply-to"] != sent.inReplyTo {
			t.Fatalf("body %q changed the header to %v", body, header)
		}
	})
}

func FuzzReadConfigFile(f *testing.F) {
	for _, content := range []string{
		"",
		"# only a comment\n\n   \n\t# indented comment\n",
		"AGENT_MAIL_SELF=alpha\n# comment\n\nAGENT_MAIL_DIR=\"/srv/mail\"\n",
		"FOO=1\n",
		"AGENT_MAIL_SELF\n",
		"=\n==\n",
		"AGENT_MAIL_SELF=\n",
		"  AGENT_MAIL_SELF = alpha  \n",
		"AGENT_MAIL_SYNC_CHECK=test -d /a=b\n",
		"AGENT_MAIL_SELF=alpha\r\nAGENT_MAIL_DIR=/srv/mail\r\n",
		"\ufeffAGENT_MAIL_SELF=alpha\n",
		"agent_mail_self=alpha\n",
	} {
		f.Add(content)
	}
	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		values, err := readConfigFile(path)
		onlyCommentsAndBlanks := true
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			onlyCommentsAndBlanks = onlyCommentsAndBlanks && (line == "" || strings.HasPrefix(line, "#"))
		}
		if onlyCommentsAndBlanks && (err != nil || len(values) > 0) {
			t.Fatalf("comments and blank lines %q gave %v, %v; want no settings", content, values, err)
		}
		if err != nil {
			return
		}
		for key := range values {
			if !configKeys[key] {
				t.Fatalf("accepted unknown setting %q from %q", key, content)
			}
		}
	})
}

func FuzzParseDeadline(f *testing.F) {
	for _, text := range []string{
		"2030-01-01 12:00",
		" 2030-01-01 12:00\n",
		"2030-02-30 12:00",
		"2028-02-29 12:00",
		"2030-02-29 12:00",
		"2030-01-01 24:00",
		"2030-1-1 1:00",
		"2030-01-01 12:00:00",
		"2030-01-01T12:00",
		"next week",
		"",
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		deadline, err := parseDeadline(text)
		if err != nil {
			return
		}
		again, err := parseDeadline(deadline.Format(deadlineLayout))
		if err != nil || !again.Equal(deadline) {
			t.Fatalf("%q parsed to %v, which does not round-trip", text, deadline)
		}
	})
}

var (
	fuzzNames = [...]string{"alpha", "beta", "gamma"}
	fuzzRoles = [...]string{
		"",
		"lead",
		"demo_grand_launch session in the JupyterHub notebook",
		"Pertamina–Toyota analist, één",
		strings.Repeat("r", 200),
	}
)

const fuzzList = "team"

var fuzzDeadline = testStart.Add(3 * time.Hour)

const (
	opJoin byte = iota
	opLeave
	opSend
	opSendList
	opListSet
	opAck
	opWait
	opListDelete
	opWho
	opInbox
	opCheck
	opListShow
	opCount
)

var opNames = [opCount]string{"join", "leave", "send", "send-list", "list-set", "ack", "wait", "list-delete", "who", "inbox", "check", "list-show"}

type step struct {
	op       byte
	actor    int
	argument byte
}

func join(actor, name, role int) step { return step{opJoin, actor, byte(name + len(fuzzNames)*role)} }
func leave(actor int) step            { return step{opLeave, actor, 0} }
func send(actor, to int) step         { return step{opSend, actor, byte(to)} }
func sendList(actor int) step         { return step{opSendList, actor, 0} }
func listSet(names ...int) step {
	var bits byte
	for _, n := range names {
		bits |= 1 << n
	}
	return step{opListSet, 0, bits}
}
func ack(actor int) step    { return step{opAck, actor, 0} }
func wait(minutes int) step { return step{opWait, 0, byte(minutes - 1)} }
func listDelete() step      { return step{opListDelete, 0, 0} }
func who(actor int) step    { return step{opWho, actor, 0} }
func inbox(actor int) step  { return step{opInbox, actor, 0} }
func check() step           { return step{opCheck, 0, 0} }
func listShow() step        { return step{opListShow, 0, 0} }
func encode(steps ...step) []byte {
	encoded := make([]byte, 0, 3*len(steps))
	for _, s := range steps {
		encoded = append(encoded, s.op, byte(s.actor), s.argument)
	}
	return encoded
}

func decode(encoded []byte) []step {
	steps := make([]step, 0, len(encoded)/3)
	for ; len(encoded) >= 3; encoded = encoded[3:] {
		steps = append(steps, step{encoded[0] % opCount, int(encoded[1]) % len(fuzzNames), encoded[2]})
	}
	return steps
}

func (s step) String() string {
	return fuzzNames[s.actor] + " " + opNames[s.op] + " " + string(rune('0'+s.argument%10))
}

const alpha, beta, gamma = 0, 1, 2

var pinnedSequences = map[string][]step{
	"mail to a member who left":                  {join(alpha, alpha, 0), join(beta, beta, 0), leave(beta), send(alpha, beta)},
	"a list naming a member who left":            {join(alpha, alpha, 0), join(beta, beta, 0), join(gamma, gamma, 0), listSet(alpha, beta, gamma), leave(gamma), sendList(alpha)},
	"an agent rejoins its own fresh name":        {join(alpha, alpha, 1), join(alpha, alpha, 2)},
	"a fresh name is taken by another agent":     {join(alpha, alpha, 0), join(beta, alpha, 0)},
	"a stale name is taken over":                 {join(alpha, alpha, 0), wait(31), join(beta, alpha, 0)},
	"a name just short of stale stays taken":     {join(alpha, alpha, 0), wait(29), join(beta, alpha, 0)},
	"mail only to oneself":                       {join(alpha, alpha, 0), send(alpha, alpha), listSet(alpha), sendList(alpha)},
	"a list with a non-member":                   {join(alpha, alpha, 0), listSet(alpha, beta)},
	"mail to a list that does not exist":         {join(alpha, alpha, 0), join(beta, beta, 0), sendList(alpha)},
	"a list deleted twice":                       {join(alpha, alpha, 0), listSet(alpha), listDelete(), listDelete()},
	"ack moves exactly one message":              {join(alpha, alpha, 0), join(beta, beta, 0), send(alpha, beta), send(alpha, beta), ack(beta), ack(beta), ack(beta)},
	"presence refreshes after five minutes":      {join(alpha, alpha, 0), join(beta, beta, 0), wait(4), send(alpha, beta), wait(2), send(alpha, beta)},
	"a rejoin keeps unread mail":                 {join(alpha, alpha, 0), join(beta, beta, 0), send(alpha, beta), leave(beta), join(beta, beta, 0), ack(beta)},
	"commands without membership are refused":    {join(alpha, alpha, 0), send(beta, alpha), leave(beta), ack(beta)},
	"who with long and non-ASCII roles":          {join(alpha, alpha, 2), join(beta, beta, 3), join(gamma, gamma, 4), who(alpha)},
	"who with nobody joined":                     {who(alpha)},
	"one agent holding two names":                {join(alpha, alpha, 0), join(alpha, beta, 0), send(alpha, beta), leave(alpha), who(gamma)},
	"mail to a list skips the sender":            {join(alpha, alpha, 0), join(beta, beta, 0), join(gamma, gamma, 0), listSet(alpha, beta, gamma), listShow(), sendList(alpha), who(alpha), ack(beta), who(gamma)},
	"lists shown before and after delete":        {listShow(), join(alpha, alpha, 0), listSet(alpha), listShow(), listDelete(), listShow()},
	"a name exactly at the stale window":         {join(alpha, alpha, 0), wait(30), who(beta), join(beta, alpha, 0)},
	"presence refreshes exactly at five minutes": {join(alpha, alpha, 0), wait(5), inbox(alpha)},
	"presence holds just before five minutes":    {join(alpha, alpha, 0), wait(4), inbox(alpha)},
	"time left pads minutes":                     {check(), wait(50), check(), wait(5), check(), wait(60), check()},
	"one minute before the deadline is live":     {join(alpha, alpha, 0), wait(60), wait(60), wait(59), check(), inbox(alpha)},
	"everything is refused at the deadline":      {join(alpha, alpha, 0), join(beta, beta, 0), listSet(alpha, beta), wait(60), wait(60), wait(60), check(), join(gamma, gamma, 0), send(alpha, beta), sendList(alpha), inbox(alpha), ack(beta), who(alpha), listDelete(), leave(alpha)},
}

func FuzzMailboxOperations(f *testing.F) {
	for _, steps := range pinnedSequences {
		f.Add(encode(steps...))
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		model := &mailboxModel{now: testStart, seen: map[string]time.Time{}, unread: map[string][]string{}, read: map[string][]string{}}
		base := newTestAgent(t, &model.now, fuzzDeadline.Format(deadlineLayout))
		for _, s := range decode(encoded) {
			model.apply(t, base, s)
			expectMailboxMatches(t, s, base.box, model)
		}
	})
}

type mailboxModel struct {
	now     time.Time
	seen    map[string]time.Time
	unread  map[string][]string
	read    map[string][]string
	list    []string
	hasList bool
}

func (m *mailboxModel) member(name string) bool {
	_, ok := m.seen[name]
	return ok
}

func (m *mailboxModel) touch(name string) {
	if m.now.Sub(m.seen[name]) >= presenceRefresh {
		m.seen[name] = m.now
	}
}

func (m *mailboxModel) apply(t *testing.T, base agent, s step) {
	t.Helper()
	actor := fuzzNames[s.actor]
	var printed bytes.Buffer
	a := base.as(actor)
	a.out = newOutput(&printed)
	if s.op == opSend || s.op == opSendList {
		m.now = m.now.Add(time.Second)
	}
	if s.op != opWait && !m.now.Before(fuzzDeadline) {
		m.expectDead(t, a, s)
		return
	}
	switch s.op {
	case opJoin:
		target := fuzzNames[int(s.argument)%len(fuzzNames)]
		role := fuzzRoles[int(s.argument)/len(fuzzNames)%len(fuzzRoles)]
		existing, taken := m.seen[target]
		allowed := !taken || target == actor || m.now.Sub(existing) >= a.cfg.staleAfter
		expectOutcome(t, s, allowed, a.dispatch("join", []string{target, role}))
		if allowed {
			m.seen[target] = m.now
		}
	case opLeave:
		expectOutcome(t, s, m.member(actor), a.dispatch("leave", nil))
		delete(m.seen, actor)
	case opSend, opSendList:
		recipients := []string{fuzzNames[int(s.argument)%len(fuzzNames)]}
		addressed := recipients[0]
		if s.op == opSendList {
			recipients, addressed = m.list, "@"+fuzzList
		}
		allowed := m.member(actor) && (s.op == opSend || m.hasList)
		others := 0
		for _, r := range recipients {
			allowed = allowed && m.member(r)
			if r != actor {
				others++
			}
		}
		allowed = allowed && others > 0
		a.stdin = strings.NewReader("body of " + s.String())
		expectOutcome(t, s, allowed, a.dispatch("send", []string{addressed, "fuzz"}))
		if !allowed {
			return
		}
		lines := strings.Split(strings.TrimRight(printed.String(), "\n"), "\n")
		id := strings.Fields(strings.TrimPrefix(lines[len(lines)-1], "sent "))[0]
		for _, r := range recipients {
			if r != actor {
				m.unread[r] = append(m.unread[r], id+".md")
			}
		}
		m.touch(actor)
	case opListSet:
		chosen := make([]string, 0, len(fuzzNames))
		for i, name := range fuzzNames {
			if s.argument&(1<<i) != 0 {
				chosen = append(chosen, name)
			}
		}
		allowed := len(chosen) > 0
		for _, name := range chosen {
			allowed = allowed && m.member(name)
		}
		expectOutcome(t, s, allowed, a.dispatch("list", append([]string{"set", fuzzList}, chosen...)))
		if allowed {
			m.list, m.hasList = chosen, true
		}
	case opAck:
		if !m.member(actor) || len(m.unread[actor]) == 0 {
			oldest := "20300101T000000Z-000000-from-nobody-none.md"
			expectOutcome(t, s, false, a.dispatch("ack", []string{oldest}))
			return
		}
		slices.Sort(m.unread[actor])
		oldest := m.unread[actor][0]
		expectOutcome(t, s, true, a.dispatch("ack", []string{oldest}))
		m.unread[actor] = m.unread[actor][1:]
		m.read[actor] = append(m.read[actor], oldest)
		m.touch(actor)
	case opWait:
		m.now = m.now.Add(time.Duration(s.argument%60+1) * time.Minute)
	case opListDelete:
		expectOutcome(t, s, m.hasList, a.dispatch("list", []string{"delete", fuzzList}))
		m.list, m.hasList = nil, false
	case opWho:
		expectOutcome(t, s, true, a.dispatch("who", nil))
		expectAlignedColumns(t, s, printed.String(), m)
	case opInbox:
		expectOutcome(t, s, m.member(actor), a.dispatch("inbox", nil))
		if m.member(actor) {
			m.touch(actor)
		}
	case opListShow:
		expectOutcome(t, s, true, a.dispatch("list", nil))
		want := ""
		if m.hasList {
			want = "@" + fuzzList + ": " + strings.Join(m.list, " ") + "\n"
		}
		if printed.String() != want {
			t.Fatalf("%s: printed %q, want %q", s, printed.String(), want)
		}
	case opCheck:
		expectOutcome(t, s, true, a.dispatch("check", nil))
		left := fuzzDeadline.Sub(m.now)
		minutes := strconv.Itoa(int(left.Minutes()) % 60)
		want := "time left: " + strconv.Itoa(int(left.Hours())) + "h " + strings.Repeat("0", 2-len(minutes)) + minutes + "m\n"
		if !strings.HasSuffix(printed.String(), want) {
			t.Fatalf("%s: printed %q, want it to end with %q", s, printed.String(), want)
		}
	}
}

func (m *mailboxModel) expectDead(t *testing.T, a agent, s step) {
	t.Helper()
	a.stdin = strings.NewReader("body")
	arguments := map[byte][]string{
		opJoin: {"gamma"}, opSend: {"beta", "late"}, opSendList: {"@" + fuzzList, "late"}, opListSet: {"set", fuzzList, "alpha"},
		opAck: {"20300101T000000Z-000000-from-nobody-none.md"}, opListDelete: {"delete", fuzzList},
	}
	commands := [opCount]string{opJoin: "join", opLeave: "leave", opSend: "send", opSendList: "send", opListSet: "list", opAck: "ack", opListDelete: "list", opWho: "who", opInbox: "inbox", opCheck: "check", opListShow: "list"}
	err := a.dispatch(commands[s.op], arguments[s.op])
	if _, dead := err.(deadError); !dead {
		t.Fatalf("%s at the deadline returned %v, want a deadError", s, err)
	}
}

func expectOutcome(t *testing.T, s step, allowed bool, err error) {
	t.Helper()
	if allowed && err != nil {
		t.Fatalf("%s: refused, want success: %v", s, err)
	}
	if !allowed && err == nil {
		t.Fatalf("%s: succeeded, want a refusal", s)
	}
}

func expectAlignedColumns(t *testing.T, s step, printed string, m *mailboxModel) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(printed, "\n"), "\n")
	header := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, "NAME") })
	if header < 0 {
		t.Fatalf("%s: who printed no header:\n%s", s, printed)
	}
	rows := lines[header+1:]
	if len(rows) != len(m.seen) {
		t.Fatalf("%s: who printed %d rows for %d members:\n%s", s, len(rows), len(m.seen), printed)
	}
	hostColumn := utf8.RuneCountInString(lines[header][:strings.Index(lines[header], "HOST")])
	for _, row := range rows {
		at := strings.Index(row, "  test-host  ")
		if at < 0 || utf8.RuneCountInString(row[:at+2]) != hostColumn {
			t.Fatalf("%s: the HOST column is out of line:\n%s", s, printed)
		}
		fields := strings.Fields(row)
		name := fields[0]
		if unread := strconv.Itoa(len(m.unread[name])); fields[len(fields)-1] != unread {
			t.Fatalf("%s: %s shows %s unread, want %s:\n%s", s, name, fields[len(fields)-1], unread, printed)
		}
		stale := m.now.Sub(m.seen[name]) >= 30*time.Minute
		if strings.Contains(row, " stale ") != stale {
			t.Fatalf("%s: %s shown stale=%v, want %v:\n%s", s, name, !stale, stale, printed)
		}
	}
}

func expectMailboxMatches(t *testing.T, s step, box mailbox, model *mailboxModel) {
	t.Helper()
	for _, name := range fuzzNames {
		m, err := readMember(box, name)
		switch {
		case model.member(name) && err != nil:
			t.Fatalf("after %s: member %s is missing: %v", s, name, err)
		case !model.member(name) && err == nil:
			t.Fatalf("after %s: %s is still a member", s, name)
		case err == nil && !m.seen.Equal(model.seen[name]):
			t.Fatalf("after %s: %s last seen %v, want %v", s, name, m.seen, model.seen[name])
		}
		expectMessages(t, s, box.inbox(name), model.unread[name])
		expectMessages(t, s, box.readDir(name), model.read[name])
	}
	err := filepath.WalkDir(box.dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".tmp") {
			t.Fatalf("after %s: temporary file left behind: %s", s, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func expectMessages(t *testing.T, s step, dir string, want []string) {
	t.Helper()
	got, _ := messagesIn(dir)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("after %s: %s holds %v, want %v", s, dir, got, want)
	}
}
