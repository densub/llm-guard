package llamacpp

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// healthTimeout bounds how long StartServer waits for llama-server to load
// its model and report healthy. Loading a few-hundred-MB GGUF model on CPU
// can take tens of seconds on first run.
const healthTimeout = 60 * time.Second

// Server manages a running llama-server subprocess.
type Server struct {
	cmd     *exec.Cmd
	BaseURL string

	exited  chan struct{}
	exitErr error
}

// StartServer launches llama-server with the given model on 127.0.0.1:port,
// streaming its stdout/stderr to logWriter, and waits for it to report
// healthy. On any failure the subprocess is killed and an error is returned.
func StartServer(serverPath, modelPath string, port int, logWriter io.Writer) (*Server, error) {
	cmd := exec.Command(serverPath,
		"-m", modelPath,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"-c", "4096",
		"--no-warmup",
	)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting llama-server: %w", err)
	}

	s := &Server{
		cmd:     cmd,
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		exited:  make(chan struct{}),
	}
	go func() {
		s.exitErr = cmd.Wait()
		close(s.exited)
	}()

	if err := s.waitHealthy(healthTimeout); err != nil {
		_ = s.Stop()
		return nil, err
	}
	return s, nil
}

func (s *Server) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-s.exited:
			return fmt.Errorf("llama-server exited before becoming healthy: %v", s.exitErr)
		default:
		}

		resp, err := client.Get(s.BaseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("llama-server did not become healthy within %s", timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Stop terminates the llama-server subprocess, waiting briefly for a clean
// exit before forcibly killing it.
func (s *Server) Stop() error {
	select {
	case <-s.exited:
		return nil
	default:
	}

	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = s.cmd.Process.Kill()
	}

	select {
	case <-s.exited:
		return nil
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exited
		return nil
	}
}
