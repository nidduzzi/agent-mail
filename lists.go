package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func readList(box mailbox, name string) ([]string, error) {
	raw, err := os.ReadFile(box.listFile(name))
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(raw)), nil
}

func (a agent) manageLists(args []string) error {
	action := "show"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "show":
		return a.showLists(args[min(1, len(args)):])
	case "set":
		if len(args) < 3 || !namePattern.MatchString(args[1]) {
			return errors.New("usage: agent-mail list set <list> <member>...")
		}
		return a.setList(args[1], args[2:])
	case "delete":
		if len(args) != 2 {
			return errors.New("usage: agent-mail list delete <list>")
		}
		if err := os.Remove(a.box.listFile(args[1])); err != nil {
			return fmt.Errorf("no list @%s", args[1])
		}
		fmt.Fprintf(a.stdout, "deleted @%s\n", args[1])
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
		if strings.HasPrefix(name, ".") || (len(only) > 0 && only[0] != name) {
			continue
		}
		members, err := readList(a.box, name)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "@%s: %s\n", name, strings.Join(members, " "))
	}
	return nil
}

func (a agent) setList(name string, members []string) error {
	var unknown []string
	for _, m := range members {
		if !isFile(a.box.memberFile(m)) {
			unknown = append(unknown, m)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("not members: %s (see agent-mail who)", strings.Join(unknown, " "))
	}
	if err := writeFileAtomically(a.box.listFile(name), strings.Join(members, "\n")+"\n"); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "@%s: %s\n", name, strings.Join(members, " "))
	return nil
}
