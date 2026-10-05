package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nidduzzi/agent-mail/internal/fakesyncthing"
)

const (
	syncSelf  = "SHKHN6V-FMTGPTV-N7BIHWV-KHFC3KS-C7U5FEB-SOP77BS-2CLXXNA-MLKHQQI"
	syncPeerA = "H32GW4R-5U7LALY-QJE52MJ-TUP5CYS-KOJKNGB-EWPXDHD-VF7ZRWV-INXSRA3"
	syncPeerB = "AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-2345672"
)

var (
	syncDevices   = [...]string{syncPeerA, syncPeerB, syncSelf, "NOT-AN-ID", "AAAAAA1-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-2345672", "aaaaaaa-bbbbbbb-ccccccc-ddddddd-eeeeeee-fffffff-ggggggg-2345672"}
	syncChecks    = [...]string{"", "syncthing", "true", "false"}
	syncAddresses = [...]string{"", "dynamic", "tcp://192.0.2.7:22000", "tcp://[2001:db8::1]:22000", "tcp://bad host:22000", "udp://192.0.2.7:22000", "relay://127.0.0.1:22067/?id=2QLRZGJ-EK4Y72N-EWGIZS6-DDFOUEP-AYXXUNI-B4NQAVW-ORMVMBZ-AE4K4AR&networkTimeout=2m0s", "relay://127.0.0.1:22067/?networkTimeout=2m0s", "relay://127.0.0.1:22067/?id=NOT-AN-ID"}
	syncNames     = [...]string{"", "notebook", "Bad_Name"}
	syncOptions   = [...]string{"global-ann-enabled", "local-ann-enabled", "relays-enabled", "natenabled"}
	syncListeners = [...][]string{
		{"default"},
		{"tcp://0.0.0.0:22000"},
		{"tcp://0.0.0.0:22000", "relay://198.51.100.4:22067"},
		{"quic://0.0.0.0:22000"},
		{"dynamic+https://relays.syncthing.net/endpoint"},
	}
)

type linkKind byte

const (
	linkAbsent linkKind = iota
	linkWaiting
	linkRelayed
	linkDirect
	linkKinds
)

const (
	syncShare byte = iota
	syncOption
	syncListen
	syncLink
	syncGarbledReport
	syncStopStart
	syncDropStignore
	syncStatus
	syncDoctor
	syncCheckMode
	syncHome
	syncClearAddresses
	syncExtraAddress
	syncUnshare
	syncOps
)

var syncOpNames = [syncOps]string{"share", "option", "listen", "link", "garbled-report", "stop-start", "drop-stignore", "status", "doctor", "check-mode", "home", "clear-addresses", "extra-address", "unshare"}

type syncStep struct {
	op      byte
	first   byte
	second  byte
	applied bool
}

func (s syncStep) String() string {
	return syncOpNames[s.op] + "(" + strconv.Itoa(int(s.first)) + "," + strconv.Itoa(int(s.second)) + "," + strconv.FormatBool(s.applied) + ")"
}

func shareStep(device, address, name int, apply bool) syncStep {
	return syncStep{syncShare, byte(device), byte(address + len(syncAddresses)*name), apply}
}
func optionStep(option int, on bool) syncStep { return syncStep{syncOption, byte(option), 0, on} }
func listenStep(choice int) syncStep          { return syncStep{syncListen, byte(choice), 0, false} }
func linkStep(device int, kind linkKind) syncStep {
	return syncStep{syncLink, byte(device), byte(kind), false}
}
func garbledReport(on bool) syncStep     { return syncStep{syncGarbledReport, 0, 0, on} }
func stopStart(stop bool) syncStep       { return syncStep{syncStopStart, 0, 0, stop} }
func dropStignore() syncStep             { return syncStep{syncDropStignore, 0, 0, false} }
func statusStep() syncStep               { return syncStep{syncStatus, 0, 0, false} }
func doctorStep() syncStep               { return syncStep{syncDoctor, 0, 0, false} }
func checkMode(mode int) syncStep        { return syncStep{syncCheckMode, byte(mode), 0, false} }
func homeStep(set bool) syncStep         { return syncStep{syncHome, 0, 0, set} }
func clearAddresses(device int) syncStep { return syncStep{syncClearAddresses, byte(device), 0, false} }
func extraAddress(device int) syncStep   { return syncStep{syncExtraAddress, byte(device), 0, false} }
func unshare(device int) syncStep        { return syncStep{syncUnshare, byte(device), 0, false} }

func encodeSync(steps ...syncStep) []byte {
	encoded := make([]byte, 0, 4*len(steps))
	for _, s := range steps {
		applied := byte(0)
		if s.applied {
			applied = 1
		}
		encoded = append(encoded, s.op, s.first, s.second, applied)
	}
	return encoded
}

func decodeSync(encoded []byte) []syncStep {
	steps := make([]syncStep, 0, len(encoded)/4)
	for ; len(encoded) >= 4; encoded = encoded[4:] {
		steps = append(steps, syncStep{encoded[0] % syncOps, encoded[1], encoded[2], encoded[3]&1 == 1})
	}
	return steps
}

const peerA, peerB, self, malformed, outsideBase32, lowercase = 0, 1, 2, 3, 4, 5
const checkUnset, checkSyncthing, checkPasses, checkFails = 0, 1, 2, 3
const noAddress, dynamicAddress, ipv4Address, ipv6Address, spacedAddress, udpAddress, relayAddress, relayWithoutID, relayWithBadID = 0, 1, 2, 3, 4, 5, 6, 7, 8
const defaultName, givenName, invalidName = 0, 1, 2
const globalDiscovery, localDiscovery, relays, natTraversal = 0, 1, 2, 3
const listenDefault, listenTCP, listenTCPAndRelay, listenQUIC, listenDynamicRelay = 0, 1, 2, 3, 4

var pinnedSyncSequences = map[string][]syncStep{
	"status before anything is shared":              {statusStep(), doctorStep()},
	"a plan changes nothing":                        {shareStep(peerA, ipv4Address, defaultName, false), statusStep(), doctorStep()},
	"share applies every step":                      {shareStep(peerA, ipv4Address, givenName, true), statusStep(), doctorStep()},
	"share without an address defaults to dynamic":  {shareStep(peerA, noAddress, defaultName, true), statusStep()},
	"share again without an address keeps it":       {shareStep(peerA, ipv4Address, defaultName, true), shareStep(peerA, noAddress, defaultName, true), statusStep()},
	"share replaces a changed address":              {shareStep(peerA, ipv4Address, defaultName, true), shareStep(peerA, dynamicAddress, defaultName, false), shareStep(peerA, dynamicAddress, defaultName, true), statusStep()},
	"share with the same address has nothing to do": {shareStep(peerA, ipv6Address, defaultName, true), shareStep(peerA, ipv6Address, defaultName, true)},
	"two peers":                                  {shareStep(peerA, ipv4Address, defaultName, true), shareStep(peerB, dynamicAddress, givenName, true), linkStep(peerB, linkRelayed), statusStep(), doctorStep()},
	"share refuses this device's own ID":         {shareStep(self, ipv4Address, defaultName, true)},
	"share refuses a malformed ID":               {shareStep(malformed, ipv4Address, defaultName, true), shareStep(outsideBase32, ipv4Address, defaultName, true), shareStep(lowercase, ipv4Address, defaultName, true)},
	"every sync check mode":                      {shareStep(peerA, ipv4Address, defaultName, true), checkMode(checkUnset), doctorStep(), checkMode(checkSyncthing), doctorStep(), checkMode(checkPasses), doctorStep(), checkMode(checkFails), doctorStep()},
	"a custom check skips a stopped Syncthing":   {checkMode(checkPasses), stopStart(true), doctorStep(), checkMode(checkFails), doctorStep(), checkMode(checkSyncthing), doctorStep()},
	"a custom check skips an unshared Syncthing": {checkMode(checkPasses), doctorStep(), shareStep(peerA, ipv4Address, defaultName, true), unshare(peerA), doctorStep(), statusStep()},
	"a Syncthing home is passed and shown":       {homeStep(true), stopStart(true), doctorStep(), stopStart(false), shareStep(peerA, ipv4Address, defaultName, true), statusStep(), homeStep(false), statusStep()},
	"a peer without addresses":                   {shareStep(peerA, dynamicAddress, defaultName, true), clearAddresses(peerA), statusStep(), doctorStep(), shareStep(peerA, ipv4Address, defaultName, true), statusStep()},
	"a peer with several addresses":              {shareStep(peerA, ipv4Address, defaultName, true), extraAddress(peerA), extraAddress(peerA), statusStep(), shareStep(peerA, ipv4Address, defaultName, true), statusStep(), extraAddress(peerA), shareStep(peerA, dynamicAddress, defaultName, true), doctorStep()},
	"the last share removed":                     {shareStep(peerA, ipv4Address, defaultName, true), shareStep(peerB, ipv4Address, defaultName, true), unshare(peerA), statusStep(), unshare(peerB), statusStep(), doctorStep()},
	"share through a private relay":              {shareStep(peerA, relayAddress, defaultName, true), statusStep(), doctorStep(), shareStep(peerA, relayWithoutID, defaultName, true), shareStep(peerA, relayWithBadID, defaultName, true)},
	"share refuses a malformed address":          {shareStep(peerA, spacedAddress, defaultName, true), shareStep(peerA, udpAddress, defaultName, true)},
	"share refuses an invalid name":              {shareStep(peerA, ipv4Address, invalidName, true)},
	"share refuses while Syncthing is stopped":   {stopStart(true), shareStep(peerA, ipv4Address, defaultName, true), statusStep(), doctorStep(), stopStart(false), statusStep()},
	"share restores a dropped stignore line":     {shareStep(peerA, ipv4Address, defaultName, true), dropStignore(), statusStep(), doctorStep(), shareStep(peerA, noAddress, defaultName, true), statusStep()},
	"links: relayed, direct, waiting and absent": {shareStep(peerA, dynamicAddress, defaultName, true), linkStep(peerA, linkRelayed), statusStep(), doctorStep(), linkStep(peerA, linkDirect), statusStep(), linkStep(peerA, linkWaiting), doctorStep(), linkStep(peerA, linkAbsent), statusStep()},
	"an unreadable connection report":            {shareStep(peerA, ipv4Address, defaultName, true), linkStep(peerA, linkRelayed), garbledReport(true), statusStep(), doctorStep(), garbledReport(false), statusStep()},
	"relays on without a relay listener":         {shareStep(peerA, dynamicAddress, defaultName, true), optionStep(relays, true), listenStep(listenTCP), doctorStep(), listenStep(listenQUIC), doctorStep(), listenStep(listenTCPAndRelay), doctorStep(), listenStep(listenDynamicRelay), doctorStep(), listenStep(listenDefault), doctorStep()},
	"a dynamic peer with discovery off":          {shareStep(peerA, dynamicAddress, defaultName, true), doctorStep(), optionStep(localDiscovery, true), doctorStep(), optionStep(localDiscovery, false), optionStep(globalDiscovery, true), doctorStep()},
	"every option shown and reported":            {shareStep(peerA, ipv4Address, defaultName, true), optionStep(globalDiscovery, true), optionStep(localDiscovery, true), optionStep(relays, true), optionStep(natTraversal, true), statusStep(), doctorStep(), optionStep(natTraversal, false), statusStep()},
}

type syncModel struct {
	running     bool
	hasFolder   bool
	ignoresTemp bool
	known       []string
	shared      []string
	addresses   map[string][]string
	names       map[string]string
	options     [len(syncOptions)]bool
	listen      []string
	links       map[string]linkKind
	garbled     bool
	check       string
	home        string
	box         string
	state       string
}

func FuzzSyncthingSharing(f *testing.F) {
	for _, steps := range pinnedSyncSequences {
		f.Add(encodeSync(steps...))
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		clock := testStart
		a := newTestAgent(t, &clock, "2031-01-01 00:00").as("alpha")
		if err := a.join([]string{"alpha"}); err != nil {
			t.Fatal(err)
		}
		state := t.TempDir()
		program := filepath.Join(state, "syncthing")
		writeFile(t, program, "")
		writeFile(t, filepath.Join(state, "id"), syncSelf+"\n")
		fake := fakesyncthing.Fake{State: state}
		a.cfg.syncthingProgram = program
		a.execute = func(_ string, argv []string) (string, bool) {
			stdout, _, status := fake.Run(argv)
			return stdout, status == 0
		}
		m := &syncModel{running: true, addresses: map[string][]string{}, names: map[string]string{}, listen: []string{"default"}, links: map[string]linkKind{}, box: a.box.dir, state: state}
		for _, s := range decodeSync(encoded) {
			m.apply(t, a, s)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (m *syncModel) apply(t *testing.T, a agent, s syncStep) {
	t.Helper()
	var printed bytes.Buffer
	a.out = newOutput(&printed)
	a.cfg.syncCheck, a.cfg.syncthingHome = m.check, m.home
	file := func(name string) string { return filepath.Join(m.state, name) }
	switch s.op {
	case syncCheckMode:
		m.check = syncChecks[int(s.first)%len(syncChecks)]
	case syncHome:
		m.home = ""
		if s.applied {
			m.home = filepath.Join(m.state, "syncthing home")
		}
	case syncClearAddresses, syncExtraAddress, syncUnshare:
		peer := syncDevices[int(s.first)%2]
		switch {
		case s.op == syncUnshare && slices.Contains(m.shared, peer):
			m.shared = slices.DeleteFunc(slices.Clone(m.shared), func(p string) bool { return p == peer })
			writeFile(t, file("shared"), strings.Join(m.shared, "\n")+"\n")
		case s.op == syncClearAddresses && slices.Contains(m.known, peer):
			m.addresses[peer] = nil
			writeFile(t, file("addresses-"+peer), "")
		case s.op == syncExtraAddress && slices.Contains(m.known, peer):
			m.addresses[peer] = append(slices.Clone(m.addresses[peer]), "tcp://198.51.100.9:22000")
			writeFile(t, file("addresses-"+peer), strings.Join(m.addresses[peer], "\n")+"\n")
		}
	case syncShare:
		m.share(t, a, s, &printed)
	case syncOption:
		option := int(s.first) % len(syncOptions)
		m.options[option] = s.applied
		writeFile(t, file("option-"+syncOptions[option]), strconv.FormatBool(s.applied)+"\n")
	case syncListen:
		m.listen = syncListeners[int(s.first)%len(syncListeners)]
		writeFile(t, file("listen"), strings.Join(m.listen, "\n")+"\n")
	case syncLink:
		m.links[syncDevices[int(s.first)%2]] = linkKind(s.second) % linkKinds
		m.writeReport(t)
	case syncGarbledReport:
		m.garbled = s.applied
		m.writeReport(t)
	case syncStopStart:
		m.running = !s.applied
		if s.applied {
			writeFile(t, file("stopped"), "")
		} else if err := os.Remove(file("stopped")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	case syncDropStignore:
		writeFile(t, filepath.Join(m.box, ".stignore"), "keep-me")
		m.ignoresTemp = false
	case syncStatus:
		err := a.dispatch("sync", []string{"status"})
		if !m.running {
			expectRefusal(t, s, err, "Syncthing is not running")
			return
		}
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if got, want := printed.String(), m.status(); got != want {
			t.Fatalf("%s: status printed\n%s\nwant\n%s", s, got, want)
		}
	case syncDoctor:
		m.doctor(t, a, s, &printed)
	}
}

func expectRefusal(t *testing.T, s syncStep, err error, reason string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("%s: returned %v, want a refusal with %q", s, err, reason)
	}
}

func (m *syncModel) writeReport(t *testing.T) {
	t.Helper()
	if m.garbled {
		writeFile(t, filepath.Join(m.state, "connections"), `{"connections": {"`+syncPeerA+`": {"connected": tru`)
		return
	}
	var report strings.Builder
	report.WriteString("{\n  \"connections\": {")
	for i, peer := range syncDevices[:2] {
		kind := m.links[peer]
		if kind == linkAbsent {
			continue
		}
		if i > 0 && report.Len() > len("{\n  \"connections\": {") {
			report.WriteString(",")
		}
		connected, kindName, address := "false", "", ""
		switch kind {
		case linkRelayed:
			connected, kindName, address = "true", "relay-server", "198.51.100.4:22067"
		case linkDirect:
			connected, kindName, address = "true", "tcp-client", "192.0.2.7:22000"
		}
		report.WriteString("\n    \"" + peer + "\": {\"address\": \"" + address + "\", \"connected\": " + connected +
			", \"primary\": {\"type\": \"" + kindName + "\", \"address\": \"" + address + "\"}, \"type\": \"" + kindName + "\"}")
	}
	report.WriteString("\n  },\n  \"total\": {}\n}\n")
	writeFile(t, filepath.Join(m.state, "connections"), report.String())
}

func (m *syncModel) share(t *testing.T, a agent, s syncStep, printed *bytes.Buffer) {
	t.Helper()
	peer := syncDevices[int(s.first)%len(syncDevices)]
	address := syncAddresses[int(s.second)%len(syncAddresses)]
	name := syncNames[int(s.second)/len(syncAddresses)%len(syncNames)]
	args := []string{"share", peer}
	if address != "" {
		args = append(args, "--address", address)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	if s.applied {
		args = append(args, "--yes")
	}
	err := a.dispatch("sync", args)
	switch {
	case slices.Index(syncDevices[:], peer) >= malformed:
		expectRefusal(t, s, err, "a device id is 8 groups")
		return
	case slices.Contains([]int{spacedAddress, udpAddress, relayWithoutID, relayWithBadID}, slices.Index(syncAddresses[:], address)):
		expectRefusal(t, s, err, "--address is")
		return
	case name == "Bad_Name":
		expectRefusal(t, s, err, "--name is")
		return
	case !m.running:
		expectRefusal(t, s, err, "Syncthing is not running")
		return
	case peer == syncSelf:
		expectRefusal(t, s, err, "this device's own ID")
		return
	case err != nil:
		t.Fatalf("%s: %v", s, err)
	}

	wanted := address
	if wanted == "" {
		wanted = "dynamic"
	}
	var steps []string
	if !slices.Contains(m.known, peer) {
		steps = append(steps, "add device "+peer)
	} else if address != "" && !slices.Equal(m.addresses[peer], []string{address}) {
		steps = append(steps, "replace the addresses of "+peer)
	}
	if !m.hasFolder {
		steps = append(steps, "add folder 'agent-mail'")
	}
	if !slices.Contains(m.shared, peer) {
		steps = append(steps, "share folder 'agent-mail' with "+peer)
	}
	if !m.ignoresTemp {
		steps = append(steps, "add '*.tmp'")
	}
	out := printed.String()
	if len(steps) == 0 {
		if !strings.HasPrefix(out, "nothing to do") {
			t.Fatalf("%s: printed %q, want nothing to do", s, out)
		}
		return
	}
	verb := "will "
	if s.applied {
		verb = "done: "
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, step := range steps {
		if i >= len(lines) || !strings.HasPrefix(lines[i], verb+step) {
			t.Fatalf("%s: printed\n%s\nwant step %d to start %q", s, out, i, verb+step)
		}
	}
	if !s.applied {
		return
	}
	if !slices.Contains(m.known, peer) {
		m.known = append(m.known, peer)
		m.addresses[peer] = []string{wanted}
		m.names[peer] = name
		if name == "" {
			m.names[peer] = "agent-mail-" + strings.ToLower(peer[:7])
		}
	} else if address != "" {
		m.addresses[peer] = []string{address}
	}
	m.hasFolder = true
	if !slices.Contains(m.shared, peer) {
		m.shared = append(m.shared, peer)
	}
	m.ignoresTemp = true
}

func (m *syncModel) linkText(peer string) string {
	if m.garbled {
		return "connection unknown"
	}
	switch m.links[peer] {
	case linkRelayed:
		return "connected via relay 198.51.100.4:22067"
	case linkDirect:
		return "connected directly (tcp-client) at 192.0.2.7:22000"
	default:
		return "not connected"
	}
}

func (m *syncModel) connected(peer string) bool {
	return !m.garbled && (m.links[peer] == linkRelayed || m.links[peer] == linkDirect)
}

func (m *syncModel) status() string {
	var want strings.Builder
	want.WriteString("this device: " + syncSelf + "\n")
	if m.hasFolder {
		want.WriteString("folder 'agent-mail': " + m.box + "\n")
	} else {
		want.WriteString("folder 'agent-mail': not configured\n")
	}
	if len(m.shared) == 0 {
		want.WriteString("shared with: nobody yet\n")
	}
	for _, peer := range m.shared {
		want.WriteString("shared with: " + peer + " (" + m.names[peer] + "), " + m.linkText(peer) + "; addresses: " + strings.Join(m.addresses[peer], " ") + "\n")
	}
	want.WriteString("this device listens on: " + strings.Join(m.listen, " ") + "\n")
	want.WriteString("global discovery " + onOff(m.options[globalDiscovery]) + ", local discovery " + onOff(m.options[localDiscovery]) +
		", relays " + onOff(m.options[relays]) + ", NAT traversal " + onOff(m.options[natTraversal]) + "\n")
	if m.ignoresTemp {
		want.WriteString(".stignore: skips *.tmp\n")
	} else {
		want.WriteString(".stignore: missing *.tmp, half-written messages could sync\n")
	}
	return want.String()
}

func (m *syncModel) doctor(t *testing.T, a agent, s syncStep, printed *bytes.Buffer) {
	t.Helper()
	err := a.dispatch("doctor", nil)
	out := printed.String()
	expect := func(present bool, text string) {
		t.Helper()
		if strings.Contains(out, text) != present {
			t.Fatalf("%s: doctor printed\n%s\nwant %q present=%v", s, out, text, present)
		}
	}
	custom := m.check == "true" || m.check == "false"
	expect(m.check == "true", "ok       sync check passes: true")
	expect(m.check == "false", "problem  sync check fails: false")
	problems := 0
	if m.check == "false" {
		problems++
	}
	serves := m.hasFolder && len(m.shared) > 0
	inspected := !custom || m.running && serves
	expect(inspected, "Syncthing found")
	switch {
	case !inspected:
	case !m.running:
		expect(true, "problem  Syncthing is not running")
		home := ""
		if m.home != "" {
			home = " --home=" + m.home
		}
		expect(true, "syncthing generate"+home+" --no-port-probing")
		problems++
	default:
		listensOnRelay := slices.ContainsFunc(m.listen, func(l string) bool {
			return l == "default" || strings.HasPrefix(l, "dynamic+") || strings.HasPrefix(l, "relay://")
		})
		relayProblem := m.options[relays] && !listensOnRelay
		for _, p := range []bool{!serves, !m.ignoresTemp, relayProblem} {
			if p {
				problems++
			}
		}
		expect(!serves, "does not share")
		expect(!m.ignoresTemp, "does not skip *.tmp")
		expect(relayProblem, "relays are on, but Syncthing listens on no relay")
		expect(slices.Contains(m.options[:], true), "may reach beyond the LAN")
		expect(m.check == "", "AGENT_MAIL_SYNC_CHECK is unset")
		connected := 0
		for _, peer := range m.shared {
			dynamicOnly := slices.Equal(m.addresses[peer], []string{"dynamic"})
			expect(dynamicOnly && !m.options[globalDiscovery] && !m.options[localDiscovery], "peer "+peer+" has only a dynamic address")
			expect(m.connected(peer), "ok       peer "+peer+" connected")
			if m.connected(peer) {
				connected++
			}
		}
		expect(len(m.shared) > 0 && connected == 0, "no peer is connected yet")
	}
	if problems == 0 {
		if err != nil {
			t.Fatalf("%s: doctor returned %v, want no problems:\n%s", s, err, out)
		}
		return
	}
	expectRefusal(t, s, err, "doctor: "+strconv.Itoa(problems)+" problems")
}
