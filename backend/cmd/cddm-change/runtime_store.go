package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type issueLock struct {
	file *os.File
}

func acquireIssueLock(repo string, issue int) (*issueLock, error) {
	runtimeDir := filepath.Join(repo, ".worktrees", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(runtimeDir, fmt.Sprintf("issue-%d.lock", issue))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another host operation is active for Issue #%d", issue)
		}
		return nil, err
	}
	return &issueLock{file: f}, nil
}

func (l *issueLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}

type processIdentity struct {
	PID        int
	PGID       int
	StartTicks string
	Cmdline    string
}

func readProcessIdentity(pid int) (processIdentity, error) {
	var out processIdentity
	if pid <= 0 {
		return out, errors.New("invalid pid")
	}
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return out, err
	}
	stat := string(statData)
	closeParen := strings.LastIndex(stat, ")")
	if closeParen < 0 || closeParen+2 >= len(stat) {
		return out, errors.New("invalid proc stat")
	}
	fields := strings.Fields(stat[closeParen+2:])
	if len(fields) <= 19 {
		return out, errors.New("short proc stat")
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return out, err
	}
	cmdlineBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return out, err
	}
	cmdline := strings.TrimSpace(strings.ReplaceAll(string(cmdlineBytes), "\x00", " "))
	return processIdentity{PID: pid, PGID: pgid, StartTicks: fields[19], Cmdline: cmdline}, nil
}

func stateOwnsProcess(state RuntimeState) (processIdentity, bool) {
	if state.ActivePID == nil || *state.ActivePID <= 0 || state.ActivePIDStartTicks == "" || state.ActivePGID == nil {
		return processIdentity{}, false
	}
	identity, err := readProcessIdentity(*state.ActivePID)
	if err != nil {
		return processIdentity{}, false
	}
	if identity.StartTicks != state.ActivePIDStartTicks || identity.PGID != *state.ActivePGID {
		return processIdentity{}, false
	}
	if !strings.Contains(identity.Cmdline, "codex") {
		return processIdentity{}, false
	}
	return identity, true
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func writeIntAtomic(path string, value int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".int-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	w := bufio.NewWriter(tmp)
	if _, err := fmt.Fprintf(w, "%d\n", value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func readIntFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || value < 0 {
		return 0, errors.New("invalid durable integer")
	}
	return value, nil
}

func readExitStatus(path string) (int, error) {
	value, err := readIntFile(path)
	if err != nil || value > 255 {
		return 0, errors.New("invalid durable exit status")
	}
	return value, nil
}
