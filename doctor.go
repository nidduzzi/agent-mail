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
	summary := "doctor: " + strconv.Itoa(problems) + " problems, " + strconv.Itoa(warnings) + " warnings; nothing was changed"
	if problems > 0 {
		return errors.New(summary)
	}
	a.out.add(summary).end()
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
	s, found := findSyncthing(a.cfg, a.execute)
	customCheck := a.cfg.syncCheck != "" && a.cfg.syncCheck != "syncthing"
	switch {
	case customCheck && shellSucceeds(a.cfg.syncCheck):
		d.note(healthy, "sync check passes: "+a.cfg.syncCheck)
	case customCheck:
		d.note(problem, "sync check fails: "+a.cfg.syncCheck, "start the sync tool, or fix AGENT_MAIL_SYNC_CHECK")
	case a.cfg.syncCheck == "" && !found:
		d.note(warning, "no sync check and no Syncthing: fine when every agent runs on this machine",
			append([]string{"to mail agents on other machines, sync " + a.box.dir + " between them:"}, installSyncthingSteps()...)...)
	case !found:
		d.note(problem, "AGENT_MAIL_SYNC_CHECK is 'syncthing' but no syncthing program was found", installSyncthingSteps()...)
	}
	if !found {
		return
	}
	state, err := s.state(a.box.dir)
	if customCheck && (err != nil || !state.servesMailbox(a.box.dir)) {
		return
	}

	d.note(healthy, "Syncthing found: "+s.program)
	if err != nil {
		d.note(problem, err.Error(), startSyncthingSteps(s, a.now(), deadline, hasDeadline)...)
		return
	}
	if state.servesMailbox(a.box.dir) {
		d.note(healthy, "Syncthing shares the mailbox with "+strconv.Itoa(len(state.sharedWith))+" peer(s)")
	} else {
		d.note(problem, "Syncthing does not share "+a.box.dir+" with any peer yet",
			"on the other machine run: agent-mail sync id",
			"then here: agent-mail sync share <their-id> --address tcp://<their-ip>:22000 (prints the plan; add --yes to apply)",
			"and the same on their side with this ID: "+state.self+" and an address of this machine they can reach",
			"a peer that can only dial out, like a container, gets --address dynamic here and this machine's address on its side",
			"across the internet: https://github.com/nidduzzi/agent-mail#-containers-and-the-internet")
	}
	if !state.ignoresTemp {
		d.note(problem, "the mailbox .stignore does not skip *.tmp, so half-written messages could sync",
			"agent-mail sync share <peer-id> --yes adds it, or add the line *.tmp to "+a.box.dir+"/.stignore")
	}
	if a.cfg.syncCheck == "" {
		d.note(warning, "AGENT_MAIL_SYNC_CHECK is unset, so a stopped Syncthing goes unnoticed",
			"set AGENT_MAIL_SYNC_CHECK=syncthing in the agent-mail config")
	}
	reach := s.reachability(state.sharedWith)
	var leaks []string
	for _, option := range [...]struct {
		enabled bool
		name    string
	}{
		{reach.globalDiscovery, "global discovery"},
		{reach.localDiscovery, "local discovery"},
		{reach.relays, "relays"},
		{reach.nat, "NAT traversal"},
	} {
		if option.enabled {
			leaks = append(leaks, option.name)
		}
	}
	if len(leaks) > 0 {
		d.note(warning, "Syncthing may reach beyond the LAN: "+strings.Join(leaks, ", ")+" enabled",
			"for a LAN-only mailbox turn them off in the Syncthing settings, and give peers explicit tcp:// addresses")
	}
	if reach.relays && !reach.listensOnRelay() {
		d.note(problem, "relays are on, but Syncthing listens on no relay ("+printable(strings.Join(reach.listen, " "))+"), so a peer that can only dial out cannot reach this device",
			"syncthing cli config options raw-listen-addresses 0 set default")
	}
	for _, p := range reach.peers {
		if p.onlyDynamic() && !reach.globalDiscovery && !reach.localDiscovery {
			d.note(warning, "peer "+p.id+" has only a dynamic address and discovery is off, so this device cannot find it; it works only when the peer connects first",
				"give its address: agent-mail sync share "+p.id+" --address tcp://<its-ip>:22000",
				"or, across the internet, turn on global discovery and relays on both sides")
		}
	}
	for _, p := range reach.peers {
		if p.link.connected {
			d.note(healthy, "peer "+p.id+" "+p.link.describe())
		}
	}
	if len(reach.peers) > 0 && reach.connectedPeers() == 0 {
		d.note(warning, "no peer is connected yet",
			"agent-mail sync status shows each peer's link and addresses",
			"check that the peer shares the folder with this device's ID, that one side can reach the other's address, and the firewall in between")
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
