package main

import (
	"errors"
	"os"
	"strings"
)

func readList(box mailbox, name string) ([]string, error) {
	if !validName(name) {
		return nil, errors.New("a list name is lowercase letters, digits and dashes")
	}
	raw, err := os.ReadFile(box.listFile(name))
	if err != nil {
		return nil, err
	}
	members := strings.Fields(string(raw))
	for _, m := range members {
		if !validName(m) {
			return nil, errors.New("@" + name + " holds an invalid member name '" + printable(m) + "'")
		}
	}
	return members, nil
}

func (a agent) manageLists(args []string) error {
	action := "show"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "show":
		if len(args) > 2 || (len(args) == 2 && !validName(args[1])) {
			return errors.New("usage: agent-mail list show [list]")
		}
		return a.showLists(args[min(1, len(args)):])
	case "set":
		if len(args) < 3 || !validName(args[1]) {
			return errors.New("usage: agent-mail list set <list> <member>...")
		}
		return a.setList(args[1], args[2:])
	case "delete":
		if len(args) != 2 || !validName(args[1]) {
			return errors.New("usage: agent-mail list delete <list>")
		}
		if err := os.Remove(a.box.listFile(args[1])); err != nil {
			return errors.New("no list @" + args[1])
		}
		a.out.add("deleted @", args[1]).end()
		return nil
	default:
		return errors.New("usage: agent-mail list [show [list] | set <list> <member>... | delete <list>]")
	}
}

func (a agent) showLists(only []string) error {
	entries, err := os.ReadDir(a.box.listsDir())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !validName(name) || (len(only) > 0 && only[0] != name) {
			continue
		}
		members, err := readList(a.box, name)
		if err != nil {
			a.out.add("@", name, ": (unreadable: ", err.Error(), ")").end()
			continue
		}
		a.out.add("@", name, ": ", strings.Join(members, " ")).end()
	}
	return nil
}

func (a agent) setList(name string, members []string) error {
	unknown := make([]string, 0, len(members))
	for _, m := range members {
		if !validName(m) || !isFile(a.box.memberFile(m)) {
			unknown = append(unknown, printable(m))
		}
	}
	if len(unknown) > 0 {
		return errors.New("not members: " + strings.Join(unknown, " ") + " (see agent-mail who)")
	}
	if err := replaceFileAtomically(a.box.listFile(name), strings.Join(members, "\n")+"\n"); err != nil {
		return err
	}
	a.out.add("@", name, ": ", strings.Join(members, " ")).end()
	return nil
}
