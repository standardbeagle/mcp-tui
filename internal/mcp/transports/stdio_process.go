package transports

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// stdioShutdownGrace is how long closing the connection waits for the server
// to exit after its stdin closes, and again after SIGTERM, before escalating
// (the spec's stdio shutdown sequence, which names no duration). The SDK's 5s
// default is paid in full by every one-shot CLI call against a server that
// keeps timers running after EOF, which is most Node servers.
const stdioShutdownGrace = 2 * time.Second

// serverStdin is the write side of a stdio server connection. Closing it ends
// the server the way the spec prescribes: close stdin, wait, SIGTERM, wait,
// SIGKILL. A server stopped by signal has shut down as prescribed, so that is
// logged as a warning about the server, not returned as a failed close.
type serverStdin struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	grace time.Duration

	closeOnce sync.Once
	closeErr  error
}

func (s *serverStdin) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *serverStdin) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.shutdown() })
	return s.closeErr
}

func (s *serverStdin) shutdown() error {
	if err := s.stdin.Close(); err != nil {
		return fmt.Errorf("closing server stdin: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()
	wait := func() (error, bool) {
		select {
		case err := <-exited:
			return err, true
		case <-time.After(s.grace):
			return nil, false
		}
	}
	if err, ok := wait(); ok {
		return err
	}
	// Signal fails where SIGTERM does not exist (Windows); go straight to kill.
	if s.cmd.Process.Signal(syscall.SIGTERM) == nil {
		if _, ok := wait(); ok {
			debug.Warn("Enhanced STDIO: server did not exit after its stdin closed; stopped with SIGTERM",
				debug.F("grace", s.grace))
			return nil
		}
	}
	if err := s.cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing server process %d: %w", s.cmd.Process.Pid, err)
	}
	if _, ok := wait(); ok {
		debug.Warn("Enhanced STDIO: server ignored stdin close and SIGTERM; killed", debug.F("grace", s.grace))
		return nil
	}
	return fmt.Errorf("server process %d did not exit after SIGKILL", s.cmd.Process.Pid)
}

// stdoutPrefixLimit bounds how much of a server's stdout is kept for
// diagnosing a failed handshake; the offending line comes first.
const stdoutPrefixLimit = 8 << 10

// stdoutPrefix keeps the first stdoutPrefixLimit bytes a server wrote to
// stdout. The SDK's reader writes to it through a TeeReader while a failed
// Connect reads it.
type stdoutPrefix struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (p *stdoutPrefix) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if room := stdoutPrefixLimit - p.buf.Len(); room > 0 {
		p.buf.Write(b[:min(len(b), room)])
	}
	return len(b), nil
}

func (p *stdoutPrefix) Bytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return bytes.Clone(p.buf.Bytes())
}

// maxQuotedLineLength bounds the stdout line quoted in an error.
const maxQuotedLineLength = 200

// firstNonJSONLine returns the first complete line of stdout that is not
// valid JSON, shortened for quoting. A line still being written (no newline
// yet) is not judged.
func firstNonJSONLine(stdout []byte) (string, bool) {
	for {
		i := bytes.IndexByte(stdout, '\n')
		if i < 0 {
			return "", false
		}
		line := bytes.TrimSpace(stdout[:i])
		stdout = stdout[i+1:]
		if len(line) == 0 || json.Valid(line) {
			continue
		}
		if len(line) > maxQuotedLineLength {
			return string(line[:maxQuotedLineLength]) + "…", true
		}
		return string(line), true
	}
}
