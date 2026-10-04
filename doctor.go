package main

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type severity int

const (
	healthy severity = iota
	warning
	problem
)

var severityLabels = [...]string{healthy: "ok", warning: "warning", problem: "problem"}

type finding struct {
	level severity
	what  string
	fix   []string
}

type diagnosis struct {
	findings []finding
}

func (d *diagnosis) note(level severity, what string, fix ...string) {
	d.findings = append(d.findings, finding{level: level, what: what, fix: fix})
}

func (d *diagnosis) count(level severity) int {
	n := 0
	for _, f := range d.findings {
		if f.level == level {
			n++
		}
	}
	return n
}

func (a agent) doctor() error {
	d := &diagnosis{findings: make([]finding, 0, 16)}
	deadline, hasDeadline := a.diagnoseMailbox(d)
	a.diagnoseSync(d, deadline, hasDeadline)

	for _, f := range d.findings {
		a.out.cell(severityLabels[f.level], 9).add(f.what).end()
		for _, step := range f.fix {
			a.out.cell("", 9).add(step).end()
		}
	}
	problems, warnings := d.count(problem), d.count(warning)
	a.out.add("doctor: ").num(int64(problems)).add(" problems, ").num(int64(warnings)).add(" warnings; nothing was changed").end()
	if problems > 0 {
		return errors.New("doctor found " + strconv.Itoa(problems) + " problems")
	}
	return nil
}

func (a agent) diagnoseMailbox(d *diagnosis) (deadline time.Time, hasDeadline bool) {
	if !isDir(a.box.membersDir()) {
		d.note(problem, "no mailbox at "+a.box.dir,
			"create one: agent-mail init [--deadline 'YYYY-MM-DD HH:MM'] (UTC)",
			"or point AGENT_MAIL_DIR at the folder the other agents use")
		return time.Time{}, false
	}
	d.note(healthy, "mailbox at "+a.box.dir)

	deadline, hasDeadline, err := a.readDeadline()
	switch {
	case err != nil:
		d.note(problem, err.Error(), "fix or remove "+a.box.deadlineFile())
	case !hasDeadline:
		d.note(healthy, "no deadline; the mailbox stays open")
	case !a.now().Before(deadline):
		d.note(problem, "the deadline "+deadline.Format(deadlineLayout)+" UTC has passed; the mailbox is closed",
			"reopening or extending it is the user's decision")
	default:
		left := deadline.Sub(a.now())
		d.note(healthy, "deadline "+deadline.Format(deadlineLayout)+" UTC, "+strconv.Itoa(int(left.Hours()))+"h left")
	}

	switch err := a.requireIdentity(); {
	case err == nil:
		d.note(healthy, "this agent is member '"+a.cfg.self+"'")
	case a.cfg.self == "":
		d.note(problem, "this agent has no identity",
			"agent-mail join <name> [role], then set AGENT_MAIL_SELF=<name> in this agent's environment")
	default:
		d.note(problem, err.Error())
	}

	if entries, err := os.ReadDir(a.box.membersDir()); err == nil {
		var stale []string
		for _, entry := range entries {
			if !validName(entry.Name()) {
				continue
			}
			if m, err := readMember(a.box, entry.Name()); err == nil && a.now().Sub(m.seen) >= a.cfg.staleAfter {
				stale = append(stale, m.name)
			}
		}
		if len(stale) > 0 {
			d.note(warning, "not seen for "+strconv.Itoa(int(a.cfg.staleAfter.Minutes()))+"+ min: "+strings.Join(stale, " "),
				"their mail waits in their inbox; if their machine is meant to be online, check its sync")
		}
	}
	return deadline, hasDeadline
}

func (a agent) diagnoseSync(d *diagnosis, deadline time.Time, hasDeadline bool) {
	s, found := findSyncthing(a.cfg)
	switch {
	case a.cfg.syncCheck == "" && !found:
		d.note(warning, "no sync check and no Syncthing: fine when every agent runs on this machine",
			append([]string{"to mail agents on other machines, sync " + a.box.dir + " between them:"}, installSyncthingSteps()...)...)
		return
	case a.cfg.syncCheck != "" && a.cfg.syncCheck != "syncthing":
		if shellSucceeds(a.cfg.syncCheck) {
			d.note(healthy, "sync check passes: "+a.cfg.syncCheck)
		} else {
			d.note(problem, "sync check fails: "+a.cfg.syncCheck, "start the sync tool, or fix AGENT_MAIL_SYNC_CHECK")
		}
		return
	case !found:
		d.note(problem, "AGENT_MAIL_SYNC_CHECK is 'syncthing' but no syncthing program was found", installSyncthingSteps()...)
		return
	}

	d.note(healthy, "Syncthing found: "+s.program)
	state, err := s.state(a.box.dir)
	if err != nil {
		d.note(problem, err.Error(), startSyncthingSteps(s, a.now(), deadline, hasDeadline)...)
		return
	}
	if state.servesMailbox(a.box.dir) {
		d.note(healthy, "Syncthing shares the mailbox with "+strconv.Itoa(len(state.sharedWith))+" peer(s)")
	} else {
		d.note(problem, "Syncthing does not share "+a.box.dir+" with any peer yet",
			"on the other machine run: agent-mail sync id",
			"then here: agent-mail sync share <their-id> --address tcp://<their-lan-ip>:22000 (prints the plan; add --yes to apply)",
			"and the same on their side with this ID: "+state.self)
	}
	if !state.ignoresTemp {
		d.note(problem, "the mailbox .stignore does not skip *.tmp, so half-written messages could sync",
			"agent-mail sync share <peer-id> --yes adds it, or add the line *.tmp to "+a.box.dir+"/.stignore")
	}
	if a.cfg.syncCheck == "" {
		d.note(warning, "AGENT_MAIL_SYNC_CHECK is unset, so a stopped Syncthing goes unnoticed",
			"set AGENT_MAIL_SYNC_CHECK=syncthing in the agent-mail config")
	}
	var leaks []string
	for _, option := range [...][2]string{
		{"global-ann-enabled", "global discovery"},
		{"local-ann-enabled", "local discovery"},
		{"relays-enabled", "relays"},
		{"natenabled", "NAT traversal"},
	} {
		if value, ok := s.run("cli", "config", "options", option[0], "get"); ok && strings.TrimSpace(value) == "true" {
			leaks = append(leaks, option[1])
		}
	}
	if len(leaks) > 0 {
		d.note(warning, "Syncthing may reach beyond the LAN: "+strings.Join(leaks, ", ")+" enabled",
			"for a LAN-only mailbox turn them off in the Syncthing settings, and give peers explicit tcp:// addresses")
	}
	d.note(warning, "agent-mail cannot see the firewall; peers need TCP 22000 from the LAN only", firewallStep())
}

func installSyncthingSteps() []string {
	verify := []string{
		"download the release archive and sha256sum.txt.asc from https://github.com/syncthing/syncthing/releases",
		"get the signing key: https://syncthing.net/release-key.txt, then: gpg --import release-key.txt",
		"verify: gpg --verify sha256sum.txt.asc, then: sha256sum -c sha256sum.txt.asc --ignore-missing",
	}
	switch runtime.GOOS {
	case "darwin":
		return append([]string{"install Syncthing: brew install syncthing, or:"}, append(verify,
			"use shasum -a 256 -c in place of sha256sum, then put syncthing on your PATH")...)
	case "windows":
		return append([]string{"install Syncthing: winget install Syncthing.Syncthing, or:"}, append(verify,
			"verify with Gpg4win and Get-FileHash, then put syncthing.exe on your PATH")...)
	default:
		return append([]string{"install Syncthing: your distribution's syncthing package, or:"}, append(verify,
			"then: install -m 755 syncthing-linux-"+runtime.GOARCH+"-*/syncthing ~/.local/bin/syncthing")...)
	}
}

func startSyncthingSteps(s syncthing, now, deadline time.Time, hasDeadline bool) []string {
	home := ""
	if s.home != "" {
		home = " --home=" + s.home
	}
	steps := []string{"first run only: syncthing generate" + home + " --no-port-probing"}
	switch runtime.GOOS {
	case "linux":
		limit := ""
		if hasDeadline && deadline.After(now) {
			limit = " --property=RuntimeMaxSec=" + strconv.Itoa(int(deadline.Sub(now).Seconds()))
		}
		steps = append(steps, "start it, stopping at the mailbox deadline: systemd-run --user --unit=agent-mail-syncthing"+limit+" "+s.program+" serve"+home+" --no-browser --no-upgrade")
	case "darwin":
		steps = append(steps, "start it: brew services start syncthing, or run: syncthing serve"+home+" --no-browser")
	default:
		steps = append(steps, "start it: syncthing serve"+home+" --no-browser")
	}
	return steps
}

func firewallStep() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS: allow syncthing in System Settings > Network > Firewall"
	case "windows":
		return "Windows: allow syncthing.exe on Private networks only"
	default:
		return "for example: sudo ufw allow from <lan-cidr> to any port 22000 proto tcp, and remove it after the deadline"
	}
}
