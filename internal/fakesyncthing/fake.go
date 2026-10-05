package fakesyncthing

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Fake struct {
	State  string
	stdout strings.Builder
	stderr strings.Builder
}

func (f *Fake) Run(args []string) (stdout, stderr string, status int) {
	f.stdout.Reset()
	f.stderr.Reset()
	withoutHome := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--home=" {
			f.stderr.WriteString("fake syncthing: --home needs a directory\n")
			return f.stdout.String(), f.stderr.String(), 2
		}
		if !strings.HasPrefix(arg, "--home=") {
			withoutHome = append(withoutHome, arg)
		}
	}
	status = f.respond(withoutHome)
	return f.stdout.String(), f.stderr.String(), status
}

func (f *Fake) respond(args []string) int {
	file := func(name string) string { return filepath.Join(f.State, name) }
	if len(args) > 0 && args[0] == "cli" && exists(file("stopped")) {
		return 1
	}
	command := strings.Join(args, " ")
	if len(args) >= 6 && strings.HasPrefix(command, "cli config devices ") && args[4] == "addresses" {
		return f.addresses(file("addresses-"+args[3]), args[5:])
	}
	if len(args) >= 5 && strings.HasPrefix(command, "cli config options raw-listen-addresses ") {
		if !exists(file("listen")) {
			replace(file("listen"), "default")
		}
		return f.addresses(file("listen"), args[4:])
	}
	switch {
	case len(args) == 6 && strings.HasPrefix(command, "cli config devices ") && args[4] == "name" && args[5] == "get":
		return f.printFile(file("name-" + args[3]))
	case command == "cli show connections":
		if f.printFile(file("connections")) != 0 {
			f.stdout.WriteString("{\"connections\": {}, \"total\": {}}\n")
		}
		return 0
	case command == "device-id":
		return f.printFile(file("id"))
	case command == "cli config folders list":
		if exists(file("folder-path")) {
			f.stdout.WriteString("agent-mail\n")
		}
		return 0
	case command == "cli config folders agent-mail path get":
		return f.printFile(file("folder-path"))
	case command == "cli config folders agent-mail devices list":
		if !exists(file("folder-path")) {
			return 1
		}
		f.printFile(file("id"))
		f.printFile(file("shared"))
		return 0
	case command == "cli config devices list":
		f.printFile(file("id"))
		f.printFile(file("devices"))
		return 0
	case strings.HasPrefix(command, "cli config devices add "):
		id := valueAfter("--device-id", args)
		replace(file("addresses-"+id), valueAfter("--addresses", args))
		replace(file("name-"+id), valueAfter("--name", args))
		return appendTo(file("devices"), id)
	case strings.HasPrefix(command, "cli config folders add "):
		return replace(file("folder-path"), valueAfter("--path", args))
	case strings.HasPrefix(command, "cli config folders agent-mail devices add "):
		return appendTo(file("shared"), valueAfter("--device-id", args))
	case len(args) == 5 && strings.HasPrefix(command, "cli config options ") && args[4] == "get":
		if f.printFile(file("option-"+args[3])) != 0 {
			f.stdout.WriteString("false\n")
		}
		return 0
	default:
		f.stderr.WriteString("fake syncthing: unsupported: " + command + "\n")
		return 64
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (f *Fake) printFile(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	f.stdout.Write(content)
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
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 1
	}
	defer file.Close()
	if _, err := file.WriteString(line + "\n"); err != nil {
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

func (f *Fake) addresses(path string, args []string) int {
	raw, _ := os.ReadFile(path)
	values := strings.Fields(string(raw))
	switch {
	case len(args) == 1 && args[0] == "list":
		for i := range values {
			f.stdout.WriteString(strconv.Itoa(i) + "\n")
		}
		return 0
	case len(args) == 2 && args[0] == "add":
		values = append(values, args[1])
	default:
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 0 || index >= len(values) || len(args) < 2 {
			return 1
		}
		switch {
		case len(args) == 2 && args[1] == "get":
			f.stdout.WriteString(values[index] + "\n")
			return 0
		case len(args) == 3 && args[1] == "set":
			values[index] = args[2]
		case len(args) == 2 && args[1] == "delete":
			values = append(values[:index], values[index+1:]...)
		default:
			return 1
		}
	}
	return replace(path, strings.Join(values, "\n"))
}
