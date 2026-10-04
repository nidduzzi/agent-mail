package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLooksLikeSecret(t *testing.T) {
	for body, secret := range map[string]bool{
		"token: abcdef":                       true,
		"token: abcde":                        false,
		"password=hunter22":                   true,
		"API_KEY = 0123456789":                true,
		"Token :\n  abcdef123":                true,
		"the token is short-lived":            false,
		"secret: <redacted>":                  false,
		"token, then token: abcdef":           true,
		"-----BEGIN RSA PRIVATE KEY-----":     true,
		"BEGIN with PRIVATE KEY far apart":    true,
		"a PRIVATE KEY is never mailed":       false,
		"passwd:":                             false,
		"private-key: abc def":                false,
		"privatekey:abcdef<":                  true,
		"no keywords here at all, just prose": false,
	} {
		if got := looksLikeSecret(body); got != secret {
			t.Errorf("looksLikeSecret(%q) = %v, want %v", body, got, secret)
		}
	}
}

func TestSendArguments(t *testing.T) {
	now := testStart
	base := newTestAgent(t, &now, "2031-01-01 00:00")
	for _, name := range []string{"alpha", "beta"} {
		if err := base.as(name).join([]string{name}); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		args    []string
		body    string
		refusal string
	}{
		{[]string{"--reply-to", "earlier-id", "beta", "answer"}, "hi", ""},
		{[]string{"--reply-to", "earlier-id"}, "hi", "usage:"},
		{[]string{"--reply-to"}, "hi", "usage:"},
		{[]string{"--reply-to", "bad id", "beta", "answer"}, "hi", "--reply-to takes a message id"},
		{[]string{"beta"}, "hi", "usage:"},
		{[]string{"beta", "hi", "extra"}, "hi", "usage:"},
		{[]string{"beta", "exactly-the-limit"}, strings.Repeat("a", maxBodyBytes), ""},
		{[]string{"beta", "over-the-limit"}, strings.Repeat("a", maxBodyBytes+1), "over 1048576 bytes"},
	} {
		now = now.Add(time.Second)
		a := base.as("alpha")
		a.stdin = strings.NewReader(c.body)
		err := a.send(c.args)
		switch {
		case c.refusal == "" && err != nil:
			t.Errorf("send %q: %v, want success", c.args, err)
		case c.refusal != "" && (err == nil || !strings.Contains(err.Error(), c.refusal)):
			t.Errorf("send %q: %v, want a refusal with %q", c.args, err, c.refusal)
		}
	}
	replies, _ := filepath.Glob(filepath.Join(base.box.inbox("beta"), "*-answer.md"))
	if len(replies) != 1 {
		t.Fatalf("found %d replies, want 1", len(replies))
	}
	header, err := readMessageHeader(replies[0])
	if err != nil || header["in-reply-to"] != "earlier-id" {
		t.Fatalf("reply header %v, %v; want in-reply-to earlier-id", header, err)
	}
}
