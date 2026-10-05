package main

import (
	"os"

	"github.com/nidduzzi/agent-mail/internal/fakesyncthing"
)

func main() {
	state := os.Getenv("FAKE_SYNCTHING_STATE")
	if state == "" {
		os.Stderr.WriteString("FAKE_SYNCTHING_STATE must name the state directory of the fake\n")
		os.Exit(2)
	}
	fake := fakesyncthing.Fake{State: state}
	stdout, stderr, status := fake.Run(os.Args[1:])
	os.Stdout.WriteString(stdout)
	os.Stderr.WriteString(stderr)
	os.Exit(status)
}
