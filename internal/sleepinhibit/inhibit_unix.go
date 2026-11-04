//go:build linux || darwin

package sleepinhibit

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Inhibitor uses a child process tied to the app's stdin pipe. If the app crashes, EOF
// releases the platform inhibitor; Linux's child trap also restores prior X11 settings.
type Inhibitor struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan error
	ownerPipe *os.File
}

type x11Settings struct {
	blanking              string
	exposures             string
	timeout               int
	cycle                 int
	standby               int
	suspend               int
	off                   int
	dpmsEnabled           bool
	dpmsAvailable         bool
	dpmsTimeoutsAvailable bool
}

func New() *Inhibitor { return &Inhibitor{} }

func (i *Inhibitor) Start() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cmd != nil {
		return nil
	}
	var command string
	var args []string
	var warnings []string
	switch runtime.GOOS {
	case "darwin":
		path, err := exec.LookPath("caffeinate")
		if err != nil {
			return fmt.Errorf("sleep/display inhibition unavailable: %w", err)
		}
		command = path
		args = []string{"-dims", "sh", "-c", "cat >/dev/null"}
	default:
		displayScript, displayErr := x11DisplayScript()
		if displayErr != nil {
			warning := displayErr.Error()
			if displayScript == "" {
				warning = "display-blanking inhibition unavailable: " + warning
			}
			warnings = append(warnings, warning)
		}
		if displayScript == "" {
			displayScript = "cat >/dev/null"
		}
		if path, err := exec.LookPath("systemd-inhibit"); err == nil {
			command = path
			args = []string{"--what=sleep:idle", "--who=Poker Clock", "--why=Tournament clock open", "--mode=block", "sh", "-c", displayScript}
		} else if displayErr == nil {
			warnings = append(warnings, "system sleep inhibition unavailable: "+err.Error())
			command = "/bin/sh"
			args = []string{"-c", displayScript}
		} else {
			return fmt.Errorf("sleep/display inhibition unavailable: %s; %v", strings.Join(warnings, "; "), err)
		}
	}
	if err := i.startManaged(command, args...); err != nil {
		return err
	}
	if len(warnings) > 0 {
		return fmt.Errorf("best-effort sleep/display inhibition: %s", strings.Join(warnings, "; "))
	}
	return nil
}

func (i *Inhibitor) startManaged(command string, args ...string) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create inhibitor lifetime pipe: %w", err)
	}
	cmd := exec.Command(command, args...)
	cmd.Stdin = reader
	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return fmt.Errorf("start sleep/display inhibitor: %w", err)
	}
	_ = reader.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_ = writer.Close()
		if err == nil {
			return fmt.Errorf("sleep/display inhibitor exited before activation")
		}
		return fmt.Errorf("sleep/display inhibitor exited before activation: %w", err)
	case <-time.After(100 * time.Millisecond):
		i.cmd = cmd
		i.done = done
		i.ownerPipe = writer
		return nil
	}
}

func x11DisplayScript() (string, error) {
	if os.Getenv("DISPLAY") == "" {
		return "", fmt.Errorf("DISPLAY is unset (Wayland/compositor support varies)")
	}
	xset, err := exec.LookPath("xset")
	if err != nil {
		return "", err
	}
	output, err := exec.Command(xset, "q").Output()
	if err != nil {
		return "", fmt.Errorf("read current X11 screen-saver settings: %w", err)
	}
	settings, err := parseX11Settings(string(output))
	if err != nil {
		return "", err
	}
	blanking := "noblank"
	if settings.blanking == "yes" {
		blanking = "blank"
	}
	exposures := "noexpose"
	if settings.exposures == "yes" {
		exposures = "expose"
	}
	disable := "xset s off"
	restore := fmt.Sprintf("xset s %d %d; xset s %s; xset s %s",
		settings.timeout, settings.cycle, blanking, exposures)
	if settings.dpmsAvailable {
		disable += " -dpms"
		if settings.dpmsTimeoutsAvailable {
			restore += fmt.Sprintf("; xset dpms %d %d %d", settings.standby, settings.suspend, settings.off)
		}
		dpms := "-dpms"
		if settings.dpmsEnabled {
			dpms = "+dpms"
		}
		restore += "; xset " + dpms
	} else {
		// Screen-saver blanking can still be inhibited; the server exposes no DPMS state to restore.
		return disable + " || exit 1; trap '" + restore + "' EXIT; trap 'exit 0' HUP INT TERM; cat >/dev/null",
			fmt.Errorf("X server does not report DPMS state; monitor power-off inhibition may be unavailable")
	}
	restore = strings.ReplaceAll(restore, "'", "'\\''")
	return disable + " || exit 1; trap '" + restore + "' EXIT; trap 'exit 0' HUP INT TERM; cat >/dev/null", nil
}

func parseX11Settings(output string) (x11Settings, error) {
	var settings x11Settings
	screen := regexp.MustCompile(`(?s)prefer blanking:\s*(yes|no)\s+allow exposures:\s*(yes|no)\s+timeout:\s*(\d+)\s+cycle:\s*(\d+)`).FindStringSubmatch(output)
	if len(screen) != 5 {
		return settings, fmt.Errorf("could not parse X11 screen-saver settings")
	}
	var err error
	settings.blanking, settings.exposures = screen[1], screen[2]
	if settings.timeout, err = strconv.Atoi(screen[3]); err != nil {
		return settings, err
	}
	if settings.cycle, err = strconv.Atoi(screen[4]); err != nil {
		return settings, err
	}
	dpmsState := regexp.MustCompile(`(?m)DPMS is (Enabled|Disabled)`).FindStringSubmatch(output)
	if len(dpmsState) == 2 {
		settings.dpmsAvailable = true
		settings.dpmsEnabled = dpmsState[1] == "Enabled"
	}
	dpmsTimeouts := regexp.MustCompile(`(?m)Standby:\s*(\d+)\s+Suspend:\s*(\d+)\s+Off:\s*(\d+)`).FindStringSubmatch(output)
	if len(dpmsTimeouts) == 4 {
		settings.dpmsTimeoutsAvailable = true
		if settings.standby, err = strconv.Atoi(dpmsTimeouts[1]); err != nil {
			return settings, err
		}
		if settings.suspend, err = strconv.Atoi(dpmsTimeouts[2]); err != nil {
			return settings, err
		}
		if settings.off, err = strconv.Atoi(dpmsTimeouts[3]); err != nil {
			return settings, err
		}
	}
	return settings, nil
}

func (i *Inhibitor) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cmd == nil {
		return nil
	}
	cmd, done, ownerPipe := i.cmd, i.done, i.ownerPipe
	i.cmd, i.done, i.ownerPipe = nil, nil, nil
	if ownerPipe != nil {
		_ = ownerPipe.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-done
			}
		}
	}
	return nil
}
