//go:build unix

package daemon

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// FindListenerPID returns the pid of a process listening on addr.
func FindListenerPID(addr string) (int, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	if host == "" {
		host = "127.0.0.1"
	}

	out, err := exec.Command(
		"lsof",
		"-nP",
		fmt.Sprintf("-iTCP@%s:%s", host, port),
		"-sTCP:LISTEN",
		"-t",
	).Output()
	if err != nil {
		return 0, fmt.Errorf("finding listener on %s: %w", addr, err)
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, fmt.Errorf("no listener on %s", addr)
	}
	// lsof may return multiple pids; take the first.
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	pid, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("parsing lsof pid %q: %w", line, err)
	}
	return pid, nil
}
