package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const syncthingFolderID = "agent-mail"

type syncthing struct {
	program string
	home    string
	invoke  func(argv []string) (output string, succeeded bool)
}

func findSyncthing(cfg config, execute programRunner) (syncthing, bool) {
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
	invoke := func(argv []string) (string, bool) { return execute(program, argv) }
	return syncthing{program: program, home: cfg.syncthingHome, invoke: invoke}, true
}

func (s syncthing) run(subcommand string, args ...string) (string, bool) {
	argv := make([]string, 0, len(args)+2)
	argv = append(argv, subcommand)
	if s.home != "" {
		argv = append(argv, "--home="+s.home)
	}
	return s.invoke(append(argv, args...))
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

func (s syncthing) listed(collection ...string) []string {
	keys, ok := s.run("cli", collection...)
	if !ok {
		return nil
	}
	values := make([]string, 0, 4)
	for _, key := range strings.Fields(keys) {
		if value, ok := s.run("cli", append(slices.Clone(collection[:len(collection)-1]), key, "get")...); ok {
			values = append(values, strings.TrimSpace(value))
		}
	}
	return values
}

func (s syncthing) optionEnabled(option string) bool {
	value, ok := s.run("cli", "config", "options", option, "get")
	return ok && strings.TrimSpace(value) == "true"
}

func (s syncthing) peerAddresses(peer string) []string {
	return s.listed("config", "devices", peer, "addresses", "list")
}

type link struct {
	known     bool
	connected bool
	kind      string
	address   string
}

func (l link) describe() string {
	switch {
	case !l.known:
		return "connection unknown"
	case !l.connected:
		return "not connected"
	case strings.HasPrefix(l.kind, "relay"):
		return "connected via relay " + printable(l.address)
	default:
		return "connected directly (" + printable(l.kind) + ") at " + printable(l.address)
	}
}

func linkFromConnections(report string, reportOK bool, peer string) link {
	if !reportOK {
		return link{}
	}
	top, ok := jsonObjectMembersRaw(report)
	if !ok {
		return link{}
	}
	connections, ok := jsonObjectMembersRaw(top["connections"])
	if !ok {
		return link{}
	}
	device, present := connections[peer]
	if !present {
		return link{known: true}
	}
	fields, ok := jsonObjectMembersRaw(device)
	if !ok {
		return link{}
	}
	return link{known: true, connected: fields["connected"] == "true", kind: fields["type"], address: fields["address"]}
}

type peerReach struct {
	id        string
	name      string
	addresses []string
	link      link
}

func (p peerReach) onlyDynamic() bool {
	return len(p.addresses) > 0 && !slices.ContainsFunc(p.addresses, func(a string) bool { return a != "dynamic" })
}

type reachability struct {
	globalDiscovery bool
	localDiscovery  bool
	relays          bool
	nat             bool
	listen          []string
	peers           []peerReach
}

func (s syncthing) reachability(peers []string) reachability {
	r := reachability{
		globalDiscovery: s.optionEnabled("global-ann-enabled"),
		localDiscovery:  s.optionEnabled("local-ann-enabled"),
		relays:          s.optionEnabled("relays-enabled"),
		nat:             s.optionEnabled("natenabled"),
		listen:          s.listed("config", "options", "raw-listen-addresses", "list"),
		peers:           make([]peerReach, 0, len(peers)),
	}
	report, reportOK := s.run("cli", "show", "connections")
	for _, peer := range peers {
		name, _ := s.run("cli", "config", "devices", peer, "name", "get")
		r.peers = append(r.peers, peerReach{
			id:        peer,
			name:      strings.TrimSpace(name),
			addresses: s.peerAddresses(peer),
			link:      linkFromConnections(report, reportOK, peer),
		})
	}
	return r
}

func (r reachability) listensOnRelay() bool {
	return slices.ContainsFunc(r.listen, func(address string) bool {
		return address == "default" || strings.HasPrefix(address, "dynamic+") || strings.HasPrefix(address, "relay://")
	})
}

func (r reachability) connectedPeers() int {
	n := 0
	for _, p := range r.peers {
		if p.link.connected {
			n++
		}
	}
	return n
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
	if !validLine(address) || strings.ContainsAny(address, " \\") {
		return false
	}
	if hostPort, isTCP := strings.CutPrefix(address, "tcp://"); isTCP {
		return hostPort != "" && !strings.Contains(hostPort, "/")
	}
	relay, isRelay := strings.CutPrefix(address, "relay://")
	hostPort, query, hasQuery := strings.Cut(relay, "/?")
	if !isRelay || !hasQuery || hostPort == "" || strings.Contains(hostPort, "/") {
		return false
	}
	for _, parameter := range strings.Split(query, "&") {
		if id, isID := strings.CutPrefix(parameter, "id="); isID {
			return validDeviceID(id)
		}
	}
	return false
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
	s, found := findSyncthing(a.cfg, a.execute)
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
	const usageLine = "usage: agent-mail sync {id | status | share <device-id> [--address tcp://ip:22000|relay://ip:22067/?id=<relay-id>|dynamic] [--name <name>] [--yes]}"
	if len(args) == 0 {
		return errors.New(usageLine)
	}
	s, found := findSyncthing(a.cfg, a.execute)
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
		a.reportSyncthing(state, s.reachability(state.sharedWith))
		return nil
	case "share":
		return a.share(s, args[1:])
	default:
		return errors.New(usageLine)
	}
}

func (a agent) reportSyncthing(state syncthingState, reach reachability) {
	a.out.add("this device: ", state.self).end()
	switch {
	case !state.hasFolder:
		a.out.add("folder '", syncthingFolderID, "': not configured").end()
	case !sameDirectory(state.folderPath, a.box.dir):
		a.out.add("folder '", syncthingFolderID, "': at ", printable(state.folderPath), ", not the mailbox ", a.box.dir).end()
	default:
		a.out.add("folder '", syncthingFolderID, "': ", a.box.dir).end()
	}
	if len(reach.peers) == 0 {
		a.out.add("shared with: nobody yet").end()
	}
	for _, p := range reach.peers {
		a.out.add("shared with: ", p.id)
		if p.name != "" {
			a.out.add(" (", printable(p.name), ")")
		}
		a.out.add(", ", p.link.describe(), "; addresses: ", printable(strings.Join(p.addresses, " "))).end()
	}
	a.out.add("this device listens on: ", printable(strings.Join(reach.listen, " "))).end()
	a.out.add("global discovery ", onOff(reach.globalDiscovery), ", local discovery ", onOff(reach.localDiscovery),
		", relays ", onOff(reach.relays), ", NAT traversal ", onOff(reach.nat)).end()
	if state.ignoresTemp {
		a.out.add(".stignore: skips *.tmp").end()
	} else {
		a.out.add(".stignore: missing *.tmp, half-written messages could sync").end()
	}
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (a agent) share(s syncthing, args []string) error {
	const usageLine = "usage: agent-mail sync share <device-id> [--address tcp://ip:22000|relay://ip:22067/?id=<relay-id>|dynamic] [--name <name>] [--yes]"
	if len(args) == 0 || !validDeviceID(args[0]) {
		return errors.New(usageLine + " (a device id is 8 groups of 7 characters A-Z, 2-7, from: agent-mail sync id)")
	}
	peer, address, addressGiven, name, apply := args[0], "dynamic", false, "", false
	for rest := args[1:]; len(rest) > 0; {
		switch {
		case rest[0] == "--yes":
			apply, rest = true, rest[1:]
		case rest[0] == "--address" && len(rest) > 1:
			address, addressGiven, rest = rest[1], true, rest[2:]
		case rest[0] == "--name" && len(rest) > 1:
			name, rest = rest[1], rest[2:]
		default:
			return errors.New(usageLine)
		}
	}
	if !validPeerAddress(address) {
		return errors.New("--address is 'dynamic', tcp://<host>:<port>, or relay://<host>:<port>/?id=<relay-id>")
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
	if current := s.peerAddresses(peer); addressGiven && slices.Contains(state.knownPeers, peer) && !slices.Equal(current, []string{address}) {
		plan = append(plan, step{"replace the addresses of " + peer + " (" + printable(strings.Join(current, " ")) + ") with " + address,
			func() error { return s.replacePeerAddresses(peer, len(current), address) }})
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
		a.out.add("nothing to do: '", syncthingFolderID, "' is already shared with ", peer, " at ", printable(strings.Join(s.peerAddresses(peer), " "))).end()
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

func (s syncthing) replacePeerAddresses(peer string, existing int, address string) error {
	addresses := []string{"config", "devices", peer, "addresses"}
	command := func(args ...string) error {
		if _, ok := s.run("cli", append(slices.Clone(addresses), args...)...); !ok {
			return errors.New("syncthing cli config devices " + peer + " addresses " + strings.Join(args, " ") + " failed")
		}
		return nil
	}
	if existing == 0 {
		return command("add", address)
	}
	if err := command("0", "set", address); err != nil {
		return err
	}
	for index := existing - 1; index >= 1; index-- {
		if err := command(strconv.Itoa(index), "delete"); err != nil {
			return err
		}
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
