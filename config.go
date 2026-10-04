package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type config struct {
	file         string
	self         string
	dir          string
	syncCheck    string
	pollInterval time.Duration
	staleAfter   time.Duration
}

var configKeys = []string{
	"AGENT_MAIL_SELF",
	"AGENT_MAIL_DIR",
	"AGENT_MAIL_SYNC_CHECK",
	"AGENT_MAIL_POLL_SECONDS",
	"AGENT_MAIL_STALE_MINUTES",
}

func loadConfig(getenv func(string) string) (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, fmt.Errorf("no home directory: %w", err)
	}
	file := getenv("AGENT_MAIL_CONFIG")
	if file == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return config{}, fmt.Errorf("no config directory: %w", err)
		}
		file = filepath.Join(configDir, "agent-mail", "config")
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
	pollSeconds, err := strconv.Atoi(value("AGENT_MAIL_POLL_SECONDS", "5"))
	if err != nil || pollSeconds < 1 {
		return config{}, errors.New("AGENT_MAIL_POLL_SECONDS must be a whole number of seconds, at least 1")
	}
	staleMinutes, err := strconv.Atoi(value("AGENT_MAIL_STALE_MINUTES", "30"))
	if err != nil || staleMinutes < 0 {
		return config{}, errors.New("AGENT_MAIL_STALE_MINUTES must be a whole number of minutes")
	}
	return config{
		file:         file,
		self:         value("AGENT_MAIL_SELF", ""),
		dir:          expandHome(value("AGENT_MAIL_DIR", filepath.Join(home, "agent-mail")), home),
		syncCheck:    value("AGENT_MAIL_SYNC_CHECK", ""),
		pollInterval: time.Duration(pollSeconds) * time.Second,
		staleAfter:   time.Duration(staleMinutes) * time.Minute,
	}, nil
}

func readConfigFile(path string) (map[string]string, error) {
	values := map[string]string{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		key, raw, found := strings.Cut(line, "=")
		if !found || strings.HasPrefix(line, "#") {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(raw), `"`)
	}
	return values, lines.Err()
}

func expandHome(path, home string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

func (c config) print(w io.Writer) {
	shown := map[string]string{
		"AGENT_MAIL_SELF":          orExplain(c.self, "unset: the name of this agent in the mailbox"),
		"AGENT_MAIL_DIR":           c.dir,
		"AGENT_MAIL_SYNC_CHECK":    orExplain(c.syncCheck, "none: command that must succeed while the sync runs"),
		"AGENT_MAIL_POLL_SECONDS":  strconv.Itoa(int(c.pollInterval / time.Second)),
		"AGENT_MAIL_STALE_MINUTES": strconv.Itoa(int(c.staleAfter / time.Minute)),
	}
	fmt.Fprintf(w, "AGENT_MAIL_CONFIG=%s\n", c.file)
	for _, key := range configKeys {
		fmt.Fprintf(w, "%s=%s\n", key, shown[key])
	}
}

func orExplain(value, explanation string) string {
	if value == "" {
		return "(" + explanation + ")"
	}
	return value
}
