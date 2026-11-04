// Package sound plays nonblocking tournament notifications with a terminal-bell fallback.
package sound

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed level-change.wav
var bundledWAV []byte

// Play starts sound playback asynchronously. It never waits for the audio backend.
// An empty path selects the embedded WAV; an invalid or unsupported sound falls back to the terminal bell.
func Play(path string, enabled bool) error {
	return play(path, enabled, bell)
}

// PlayDesktop plays a notification without writing a terminal bell. GUI callers surface
// playback failures through their normal on-screen status instead.
func PlayDesktop(path string, enabled bool) error {
	return play(path, enabled, nil)
}

func play(path string, enabled bool, fallback func()) error {
	if !enabled {
		return nil
	}
	beep := func() {
		if fallback != nil {
			fallback()
		}
	}
	cleanup := func() {}
	if path == "" {
		temporary, err := os.CreateTemp("", "poker-clock-*.wav")
		if err != nil {
			beep()
			return fmt.Errorf("create bundled notification sound: %w", err)
		}
		name := temporary.Name()
		cleanup = func() { _ = os.Remove(name) }
		if _, err := temporary.Write(bundledWAV); err != nil {
			_ = temporary.Close()
			cleanup()
			beep()
			return fmt.Errorf("write bundled notification sound: %w", err)
		}
		if err := temporary.Close(); err != nil {
			cleanup()
			beep()
			return fmt.Errorf("close bundled notification sound: %w", err)
		}
		path = name
	} else if ext := strings.ToLower(filepath.Ext(path)); ext != ".wav" && ext != ".mp3" {
		beep()
		return fmt.Errorf("sound file must be WAV or MP3")
	} else if _, err := os.Stat(path); err != nil {
		beep()
		return fmt.Errorf("open selected sound file: %w", err)
	}

	command, args, err := playerCommand(path)
	if err != nil {
		cleanup()
		beep()
		return err
	}
	cmd := exec.Command(command, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		cleanup()
		beep()
		return fmt.Errorf("start sound playback: %w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			beep()
		}
		cleanup()
	}()
	return nil
}

func playerCommand(path string) (string, []string, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".wav" && extension != ".mp3" {
		return "", nil, fmt.Errorf("sound file must be WAV or MP3")
	}
	var candidates []struct {
		name string
		args []string
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, struct {
			name string
			args []string
		}{"afplay", []string{path}})
	case "windows":
		candidates = append(candidates, struct {
			name string
			args []string
		}{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", "-nostdin", path}})
		if strings.EqualFold(filepath.Ext(path), ".wav") {
			quoted := strings.ReplaceAll(path, "'", "''")
			candidates = append(candidates, struct {
				name string
				args []string
			}{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", "(New-Object System.Media.SoundPlayer -ArgumentList '" + quoted + "').PlaySync()"}})
		}
	default:
		candidates = append(candidates,
			struct {
				name string
				args []string
			}{"paplay", []string{path}},
			struct {
				name string
				args []string
			}{"aplay", []string{"-q", path}},
			struct {
				name string
				args []string
			}{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", "-nostdin", path}},
		)
	}
	for _, candidate := range candidates {
		if executable, err := exec.LookPath(candidate.name); err == nil {
			return executable, candidate.args, nil
		}
	}
	return "", nil, fmt.Errorf("no supported sound player is available")
}

func bell() { _, _ = fmt.Fprint(os.Stderr, "\a") }
