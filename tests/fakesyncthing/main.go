package main

import (
	"os"
	"path/filepath"
	"strings"
)

func main() {
	state := os.Getenv("FAKE_SYNCTHING_STATE")
	if state == "" {
		os.Stderr.WriteString("FAKE_SYNCTHING_STATE must name the state directory of the fake\n")
		os.Exit(2)
	}
	args := make([]string, 0, len(os.Args))
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "--home=") {
			args = append(args, arg)
		}
	}
	os.Exit(respond(state, args))
}

func respond(state string, args []string) int {
	file := func(name string) string { return filepath.Join(state, name) }
	if len(args) > 0 && args[0] == "cli" && exists(file("stopped")) {
		return 1
	}
	command := strings.Join(args, " ")
	switch {
	case command == "device-id":
		return printFile(file("id"))
	case command == "cli config folders list":
		if exists(file("folder-path")) {
			os.Stdout.WriteString("agent-mail\n")
		}
		return 0
	case command == "cli config folders agent-mail path get":
		return printFile(file("folder-path"))
	case command == "cli config folders agent-mail devices list":
		if !exists(file("folder-path")) {
			return 1
		}
		printFile(file("id"))
		printFile(file("shared"))
		return 0
	case command == "cli config devices list":
		printFile(file("id"))
		printFile(file("devices"))
		return 0
	case strings.HasPrefix(command, "cli config devices add "):
		return appendTo(file("devices"), valueAfter("--device-id", args))
	case strings.HasPrefix(command, "cli config folders add "):
		return replace(file("folder-path"), valueAfter("--path", args))
	case strings.HasPrefix(command, "cli config folders agent-mail devices add "):
		return appendTo(file("shared"), valueAfter("--device-id", args))
	case len(args) == 5 && strings.HasPrefix(command, "cli config options ") && args[4] == "get":
		if printFile(file("option-"+args[3])) != 0 {
			os.Stdout.WriteString("false\n")
		}
		return 0
	default:
		os.Stderr.WriteString("fake syncthing: unsupported: " + command + "\n")
		return 64
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func printFile(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	os.Stdout.Write(content)
	return 0
}

func valueAfter(flag string, args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func appendTo(path, line string) int {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 1
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return 1
	}
	return 0
}

func replace(path, line string) int {
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		return 1
	}
	return 0
}
