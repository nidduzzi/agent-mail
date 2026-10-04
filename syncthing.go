package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const syncthingFolderID = "agent-mail"

type syncthing struct {
	program string
	home    string
}

func findSyncthing(cfg config) (syncthing, bool) {
	program := cfg.syncthingProgram
	if program == "" {
		found, ok := findProgram("syncthing")
		if !ok {
			return syncthing{}, false
		}
		program = found
	} else if !isFile(program) {
		return syncthing{}, false
	}
	return syncthing{program: program, home: cfg.syncthingHome}, true
}

func (s syncthing) run(subcommand string, args ...string) (string, bool) {
	argv := make([]string, 0, len(args)+2)
	argv = append(argv, subcommand)
	if s.home != "" {
		argv = append(argv, "--home="+s.home)
	}
	return runProgram(s.program, append(argv, args...))
}

func (s syncthing) deviceIDs(args ...string) ([]string, bool) {
	out, ok := s.run("cli", args...)
	ids := strings.Fields(out)
	valid := ids[:0]
	for _, id := range ids {
		if validDeviceID(id) {
			valid = append(valid, id)
		}
	}
	return valid, ok
}

func (s syncthing) deviceID() (string, bool) {
	out, ok := s.run("device-id")
	id := strings.TrimSpace(out)
	return id, ok && validDeviceID(id)
}

func (s syncthing) folderPath() (string, bool) {
	out, ok := s.run("cli", "config", "folders", syncthingFolderID, "path", "get")
	return strings.TrimSpace(out), ok
}

func (s syncthing) running() bool {
	_, ok := s.run("cli", "config", "folders", "list")
	return ok
}

func (s syncthing) devices() ([]string, bool) {
	return s.deviceIDs("config", "devices", "list")
}

func (s syncthing) folderDevices() ([]string, bool) {
	return s.deviceIDs("config", "folders", syncthingFolderID, "devices", "list")
}

func validDeviceID(id string) bool {
	groups := strings.Split(id, "-")
	if len(groups) != 8 {
		return false
	}
	for _, group := range groups {
		if len(group) != 7 {
			return false
		}
		for i := 0; i < len(group); i++ {
			c := group[i]
			if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
				return false
			}
		}
	}
	return true
}

func validPeerAddress(address string) bool {
	if address == "dynamic" {
		return true
	}
	hostPort, isTCP := strings.CutPrefix(address, "tcp://")
	if !isTCP || hostPort == "" || !validLine(hostPort) {
		return false
	}
	return !strings.ContainsAny(hostPort, " /\\")
}

type syncthingState struct {
	self        string
	folderPath  string
	hasFolder   bool
	sharedWith  []string
	knownPeers  []string
	ignoresTemp bool
}

func (s syncthing) state(mailboxDir string) (syncthingState, error) {
	self, ok := s.deviceID()
	if !ok {
		return syncthingState{}, errors.New("cannot read this device's Syncthing ID")
	}
	if !s.running() {
		return syncthingState{}, errors.New("Syncthing is not running (its CLI cannot reach it)")
	}
	state := syncthingState{self: self, ignoresTemp: ignoresTemporaryFiles(mailboxDir)}
	devices, _ := s.devices()
	for _, d := range devices {
		if d != self {
			state.knownPeers = append(state.knownPeers, d)
		}
	}
	state.folderPath, state.hasFolder = s.folderPath()
	if state.hasFolder {
		shared, _ := s.folderDevices()
		for _, d := range shared {
			if d != self {
				state.sharedWith = append(state.sharedWith, d)
			}
		}
	}
	return state, nil
}

func ignoresTemporaryFiles(mailboxDir string) bool {
	raw, err := os.ReadFile(filepath.Join(mailboxDir, ".stignore"))
	if err != nil {
		return false
	}
	return slices.Contains(strings.Fields(string(raw)), "*.tmp")
}

func sameDirectory(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func (st syncthingState) servesMailbox(mailboxDir string) bool {
	return st.hasFolder && sameDirectory(st.folderPath, mailboxDir) && len(st.sharedWith) > 0
}

func (a agent) syncthingServesMailbox() error {
	s, found := findSyncthing(a.cfg)
	if !found {
		return deadError{"the sync check is 'syncthing' but no syncthing program was found (see agent-mail doctor)"}
	}
	state, err := s.state(a.box.dir)
	if err != nil {
		return deadError{"the sync check failed: " + err.Error()}
	}
	if !state.servesMailbox(a.box.dir) {
		return deadError{"the sync check failed: Syncthing does not share " + a.box.dir + " as folder '" + syncthingFolderID + "' with any peer (see agent-mail sync status)"}
	}
	return nil
}

func (a agent) syncCommand(args []string) error {
	const usageLine = "usage: agent-mail sync {id | status | share <device-id> [--address tcp://ip:22000|dynamic] [--name <name>] [--yes]}"
	if len(args) == 0 {
		return errors.New(usageLine)
	}
	s, found := findSyncthing(a.cfg)
	if !found {
		return errors.New("no syncthing program found on the PATH or at AGENT_MAIL_SYNCTHING; run agent-mail doctor for install steps")
	}
	switch args[0] {
	case "id":
		id, ok := s.deviceID()
		if !ok {
			return errors.New("cannot read this device's Syncthing ID")
		}
		a.out.add(id).end()
		return nil
	case "status":
		state, err := s.state(a.box.dir)
		if err != nil {
			return err
		}
		a.reportSyncthing(state)
		return nil
	case "share":
		return a.share(s, args[1:])
	default:
		return errors.New(usageLine)
	}
}

func (a agent) reportSyncthing(state syncthingState) {
	a.out.add("this device: ", state.self).end()
	switch {
	case !state.hasFolder:
		a.out.add("folder '", syncthingFolderID, "': not configured").end()
	case !sameDirectory(state.folderPath, a.box.dir):
		a.out.add("folder '", syncthingFolderID, "': at ", printable(state.folderPath), ", not the mailbox ", a.box.dir).end()
	default:
		a.out.add("folder '", syncthingFolderID, "': ", a.box.dir).end()
	}
	if len(state.sharedWith) == 0 {
		a.out.add("shared with: nobody yet").end()
	} else {
		a.out.add("shared with: ", strings.Join(state.sharedWith, " ")).end()
	}
	if state.ignoresTemp {
		a.out.add(".stignore: skips *.tmp").end()
	} else {
		a.out.add(".stignore: missing *.tmp, half-written messages could sync").end()
	}
}

func (a agent) share(s syncthing, args []string) error {
	const usageLine = "usage: agent-mail sync share <device-id> [--address tcp://ip:22000|dynamic] [--name <name>] [--yes]"
	if len(args) == 0 || !validDeviceID(args[0]) {
		return errors.New(usageLine + " (a device id is 8 groups of 7 characters A-Z, 2-7, from: agent-mail sync id)")
	}
	peer, address, name, apply := args[0], "dynamic", "", false
	for rest := args[1:]; len(rest) > 0; {
		switch {
		case rest[0] == "--yes":
			apply, rest = true, rest[1:]
		case rest[0] == "--address" && len(rest) > 1:
			address, rest = rest[1], rest[2:]
		case rest[0] == "--name" && len(rest) > 1:
			name, rest = rest[1], rest[2:]
		default:
			return errors.New(usageLine)
		}
	}
	if !validPeerAddress(address) {
		return errors.New("--address is 'dynamic' or tcp://<host>:<port>")
	}
	if name == "" {
		name = "agent-mail-" + strings.ToLower(peer[:7])
	}
	if !validName(name) {
		return errors.New("--name is lowercase letters, digits and dashes")
	}
	state, err := s.state(a.box.dir)
	if err != nil {
		return err
	}
	if peer == state.self {
		return errors.New("that is this device's own ID; share with the other machine's ID")
	}
	if state.hasFolder && !sameDirectory(state.folderPath, a.box.dir) {
		return errors.New("Syncthing folder '" + syncthingFolderID + "' already points at " + printable(state.folderPath) + ", not " + a.box.dir + "; change it in Syncthing first")
	}

	type step struct {
		describe string
		apply    func() error
	}
	runStep := func(subcommand string, args ...string) func() error {
		return func() error {
			if _, ok := s.run(subcommand, args...); !ok {
				return errors.New("syncthing " + subcommand + " " + strings.Join(args, " ") + " failed")
			}
			return nil
		}
	}
	var plan []step
	if !slices.Contains(state.knownPeers, peer) {
		plan = append(plan, step{"add device " + peer + " as " + name + " at " + address,
			runStep("cli", "config", "devices", "add", "--device-id", peer, "--name", name, "--addresses", address)})
	}
	if !state.hasFolder {
		plan = append(plan, step{"add folder '" + syncthingFolderID + "' at " + a.box.dir + ", checking for changes every second",
			runStep("cli", "config", "folders", "add", "--id", syncthingFolderID, "--label", syncthingFolderID, "--path", a.box.dir, "--fswatcher-delays", "1")})
	}
	if !slices.Contains(state.sharedWith, peer) {
		plan = append(plan, step{"share folder '" + syncthingFolderID + "' with " + peer,
			runStep("cli", "config", "folders", syncthingFolderID, "devices", "add", "--device-id", peer)})
	}
	if !state.ignoresTemp {
		plan = append(plan, step{"add '*.tmp' to " + filepath.Join(a.box.dir, ".stignore") + " so half-written messages never sync",
			func() error { return appendLine(filepath.Join(a.box.dir, ".stignore"), "*.tmp") }})
	}

	if len(plan) == 0 {
		a.out.add("nothing to do: '", syncthingFolderID, "' is already shared with ", peer).end()
		return nil
	}
	for _, s := range plan {
		if apply {
			if err := s.apply(); err != nil {
				return err
			}
			a.out.add("done: ", s.describe).end()
		} else {
			a.out.add("will ", s.describe).end()
		}
	}
	if !apply {
		a.out.add("nothing changed yet; run the same command with --yes to apply").end()
	}
	return nil
}

func appendLine(path, line string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		line = "\n" + line
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
