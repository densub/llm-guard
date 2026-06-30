package install

import (
	"io"
	"os"

	"golang.org/x/term"
)

// terminalIO is an interactive stdin/stdout pair, usually /dev/tty.
type terminalIO struct {
	in  *os.File
	out *os.File
}

func (t terminalIO) Read(p []byte) (int, error)  { return t.in.Read(p) }
func (t terminalIO) Write(p []byte) (int, error) { return t.out.Write(p) }
func (t terminalIO) Fd() uintptr                 { return t.in.Fd() }

func openTerminalIO() (terminalIO, error) {
	in, err := os.Open("/dev/tty")
	if err != nil {
		return terminalIO{}, err
	}
	return terminalIO{in: in, out: in}, nil
}

func isTerminalFile(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func isInteractiveReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return isTerminalFile(f)
}
