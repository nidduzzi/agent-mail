package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type config struct {
	file              string
	self              string
	dir               string
	syncCheck         string
	syncCheckInterval time.Duration
	syncthingProgram  string
	syncthingHome     string
	pollInterval      time.Duration
	staleAfter        time.Duration
}

var configKeys = map[string]bool{
	"AGENT_MAIL_SELF":               true,
	"AGENT_MAIL_DIR":                true,
	"AGENT_MAIL_SYNC_CHECK":         true,
	"AGENT_MAIL_SYNC_CHECK_SECONDS": true,
	"AGENT_MAIL_POLL_SECONDS":       true,
	"AGENT_MAIL_STALE_MINUTES":      true,
	"AGENT_MAIL_SYNCTHING":          true,
	"AGENT_MAIL_SYNCTHING_HOME":     true,
}

func loadConfig(getenv func(string) string) (config, error) {
	file := getenv("AGENT_MAIL_CONFIG")
	if file == "" {
		if configDir, err := os.UserConfigDir(); err == nil {
			file = filepath.Join(configDir, "agent-mail", "config")
		}
	}
	fromFile, err := readConfigFile(file)
	if err != nil {
		return config{}, err
	}
	value := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		if v := fromFile[key]; v != "" {
			return v
		}
		return fallback
	}
	home, _ := os.UserHomeDir()
	path := func(key, fallback string) (string, error) {
		p := value(key, fallback)
		if p != "~" && !strings.HasPrefix(p, "~/") {
			return p, nil
		}
		if home == "" {
			return "", errors.New(key + " starts with ~ but the home directory is unknown; give an absolute path")
		}
		return filepath.Join(home, p[1:]), nil
	}
	duration := func(key, fallback string, minimum int, unit time.Duration) (time.Duration, error) {
		n, err := strconv.Atoi(value(key, fallback))
		if err != nil || n < minimum {
			return 0, errors.New(key + " must be a whole number, at least " + strconv.Itoa(minimum))
		}
		return time.Duration(n) * unit, nil
	}
	poll, err := duration("AGENT_MAIL_POLL_SECONDS", "5", 1, time.Second)
	if err != nil {
		return config{}, err
	}
	syncEvery, err := duration("AGENT_MAIL_SYNC_CHECK_SECONDS", "60", 1, time.Second)
	if err != nil {
		return config{}, err
	}
	staleAfter, err := duration("AGENT_MAIL_STALE_MINUTES", "30", 0, time.Minute)
	if err != nil {
		return config{}, err
	}
	dir, err := path("AGENT_MAIL_DIR", "~/agent-mail")
	if err != nil {
		return config{}, err
	}
	if !filepath.IsAbs(dir) {
		return config{}, errors.New("AGENT_MAIL_DIR must be an absolute path, so every agent finds the same mailbox: " + dir)
	}
	syncthingProgram, err := path("AGENT_MAIL_SYNCTHING", "")
	if err != nil {
		return config{}, err
	}
	syncthingHome, err := path("AGENT_MAIL_SYNCTHING_HOME", "")
	if err != nil {
		return config{}, err
	}
	return config{
		file:              file,
		self:              value("AGENT_MAIL_SELF", ""),
		dir:               dir,
		syncCheck:         value("AGENT_MAIL_SYNC_CHECK", ""),
		syncCheckInterval: syncEvery,
		syncthingProgram:  syncthingProgram,
		syncthingHome:     syncthingHome,
		pollInterval:      poll,
		staleAfter:        staleAfter,
	}, nil
}

func readConfigFile(path string) (map[string]string, error) {
	values := make(map[string]string, len(configKeys))
	if path == "" {
		return values, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, errors.New("cannot read " + path + ": " + err.Error())
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rawKey, rawValue, found := strings.Cut(line, "=")
		key := strings.TrimSpace(rawKey)
		if !found || !configKeys[key] {
			return nil, errors.New(path + ": unknown setting '" + printable(rawKey) + "' (see agent-mail config)")
		}
		values[key] = strings.Trim(strings.TrimSpace(rawValue), `"`)
	}
	return values, nil
}

func (c config) print(o *output) {
	settings := [...][2]string{
		{"AGENT_MAIL_CONFIG", orExplain(c.file, "none: no config directory here")},
		{"AGENT_MAIL_SELF", orExplain(c.self, "unset: the name of this agent in the mailbox")},
		{"AGENT_MAIL_DIR", c.dir},
		{"AGENT_MAIL_SYNC_CHECK", orExplain(c.syncCheck, "none: 'syncthing', or a command that must succeed while the sync runs")},
		{"AGENT_MAIL_SYNC_CHECK_SECONDS", strconv.Itoa(int(c.syncCheckInterval / time.Second))},
		{"AGENT_MAIL_POLL_SECONDS", strconv.Itoa(int(c.pollInterval / time.Second))},
		{"AGENT_MAIL_STALE_MINUTES", strconv.Itoa(int(c.staleAfter / time.Minute))},
		{"AGENT_MAIL_SYNCTHING", orExplain(c.syncthingProgram, "unset: syncthing from the PATH")},
		{"AGENT_MAIL_SYNCTHING_HOME", orExplain(c.syncthingHome, "unset: the Syncthing default")},
	}
	for _, setting := range settings {
		o.add(setting[0], "=", setting[1]).end()
	}
}

func orExplain(value, explanation string) string {
	if value == "" {
		return "(" + explanation + ")"
	}
	return value
}
