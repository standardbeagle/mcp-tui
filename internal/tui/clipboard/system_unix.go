//go:build unix

package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// helper is one clipboard program pair. atotto/clipboard runs the same
// programs but with no way to stop one, so a hung helper leaked forever.
type helper struct {
	copy, paste []string
	trimCRLF    bool // the paste program ends its output with \r\n
}

// helpers are tried in atotto/clipboard's order; wayland only counts when
// WAYLAND_DISPLAY is set.
var (
	pbHelper      = helper{copy: []string{"pbcopy"}, paste: []string{"pbpaste"}}
	waylandHelper = helper{copy: []string{"wl-copy"}, paste: []string{"wl-paste", "--no-newline"}}
	otherHelpers  = []helper{
		{copy: []string{"xclip", "-in", "-selection", "clipboard"}, paste: []string{"xclip", "-out", "-selection", "clipboard"}},
		{copy: []string{"xsel", "--input", "--clipboard"}, paste: []string{"xsel", "--output", "--clipboard"}},
		{copy: []string{"termux-clipboard-set"}, paste: []string{"termux-clipboard-get"}},
		{copy: []string{"clip.exe"}, paste: []string{"powershell.exe", "Get-Clipboard"}, trimCRLF: true},
	}
)

// chooseHelper picks the first helper whose programs are installed.
func chooseHelper(goos string, wayland bool, installed func(string) bool) (helper, error) {
	candidates := otherHelpers
	switch {
	case goos == "darwin":
		candidates = []helper{pbHelper}
	case wayland:
		candidates = append([]helper{waylandHelper}, otherHelpers...)
	}
	for _, h := range candidates {
		if installed(h.copy[0]) && installed(h.paste[0]) {
			return h, nil
		}
	}
	return helper{}, errors.New("no clipboard program found (install wl-clipboard, xclip or xsel)")
}

var systemHelper = sync.OnceValues(func() (helper, error) {
	return chooseHelper(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY") != "", func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
})

// systemClipboard runs the platform's clipboard programs, killed at ctx's
// deadline.
type systemClipboard struct{}

func (systemClipboard) Write(ctx context.Context, text string) error {
	h, err := systemHelper()
	if err != nil {
		return err
	}
	_, err = runHelper(ctx, h.copy, text, false)
	return err
}

func (systemClipboard) Read(ctx context.Context) (string, error) {
	h, err := systemHelper()
	if err != nil {
		return "", err
	}
	out, err := runHelper(ctx, h.paste, "", true)
	if h.trimCRLF {
		out = strings.TrimSuffix(out, "\r\n")
	}
	return out, err
}

// runHelper runs args with stdin as its input and returns its stdout when
// readStdout is set. At ctx's deadline the helper's whole process group is
// killed: xclip and wl-copy fork a child that keeps the selection, and a
// hung one must not outlive the copy.
//
// Stdout and stderr are left unattached on a copy: the forked child inherits
// them, and Wait would wait for it to close them.
func runHelper(ctx context.Context, args []string, stdin string, readStdout bool) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 100 * time.Millisecond

	var out bytes.Buffer
	if readStdout {
		cmd.Stdout = &out
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", args[0], err)
	}
	return out.String(), nil
}
