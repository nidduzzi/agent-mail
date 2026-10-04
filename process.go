package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const maxProgramOutput = 1 << 20

func findProgram(name string) (string, bool) {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name+suffix)
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
			return candidate, true
		}
	}
	return "", false
}

func runProgram(program string, args []string) (output string, succeeded bool) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return "", false
	}
	defer devNull.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", false
	}
	defer reader.Close()
	argv := make([]string, 0, len(args)+1)
	argv = append(append(argv, program), args...)
	process, err := os.StartProcess(program, argv, &os.ProcAttr{Files: []*os.File{devNull, writer, devNull}})
	writer.Close()
	if err != nil {
		return "", false
	}
	captured, readErr := io.ReadAll(io.LimitReader(reader, maxProgramOutput))
	state, waitErr := process.Wait()
	return string(captured), readErr == nil && waitErr == nil && state.Success()
}

func shellSucceeds(command string) bool {
	if runtime.GOOS == "windows" {
		shell := os.Getenv("ComSpec")
		if shell == "" {
			shell = `C:\Windows\System32\cmd.exe`
		}
		_, ok := runProgram(shell, []string{"/C", command})
		return ok
	}
	_, ok := runProgram("/bin/sh", []string{"-c", command})
	return ok
}
