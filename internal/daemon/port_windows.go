//go:build windows

package daemon

import "fmt"

// FindListenerPID returns the pid of a process listening on addr.
func FindListenerPID(addr string) (int, error) {
	return 0, fmt.Errorf("finding listener on %s without a pidfile is not supported on Windows", addr)
}
