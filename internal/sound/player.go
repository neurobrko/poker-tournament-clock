// Package sound plays nonblocking tournament notifications.
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

//go:embed level-change.mp3
var bundledLevelMP3 []byte

//go:embed minute-before-level.wav
var bundledMinuteWAV []byte

// PlayDesktop plays a configured or bundled MP3 asynchronously. Playback failures are reported without blocking the UI.
func PlayDesktop(path string, enabled bool, onError func(error)) error {
	return play(path, enabled, bundledLevelMP3, ".mp3", onError)
}

// PlayMinuteWarningDesktop plays the configured or bundled one-minute warning asynchronously.
func PlayMinuteWarningDesktop(path string, enabled bool, onError func(error)) error {
	return play(path, enabled, bundledMinuteWAV, ".wav", onError)
}

func play(path string, enabled bool, defaultAudio []byte, defaultExtension string, onError func(error)) error {
	if !enabled {
		return nil
	}
	cleanup := func() {}
	if path == "" {
		temporary, err := os.CreateTemp("", "poker-clock-*"+defaultExtension)
		if err != nil {
			return fmt.Errorf("create bundled notification sound: %w", err)
		}
		name := temporary.Name()
		cleanup = func() { _ = os.Remove(name) }
		if _, err := temporary.Write(defaultAudio); err != nil {
			_ = temporary.Close()
			cleanup()
			return fmt.Errorf("write bundled notification sound: %w", err)
		}
		if err := temporary.Close(); err != nil {
			cleanup()
			return fmt.Errorf("close bundled notification sound: %w", err)
		}
		path = name
	} else if ext := strings.ToLower(filepath.Ext(path)); ext != ".wav" && ext != ".mp3" {
		return fmt.Errorf("sound file must be WAV or MP3")
	} else if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("open selected sound file: %w", err)
	}

	command, args, err := playerCommand(path)
	if err != nil {
		cleanup()
		return err
	}
	cmd := exec.Command(command, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		cleanup()
		return fmt.Errorf("start sound playback: %w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil && onError != nil {
			onError(fmt.Errorf("sound playback failed: %w", err))
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
		}{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}})
		quoted := strings.ReplaceAll(path, "'", "''")
		if extension == ".wav" {
			candidates = append(candidates, struct {
				name string
				args []string
			}{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", "(New-Object System.Media.SoundPlayer -ArgumentList '" + quoted + "').PlaySync()"}})
		} else {
			player := "$p = New-Object -ComObject WMPlayer.OCX; $p.URL = '" + quoted + "'; $p.controls.play(); $sw = [Diagnostics.Stopwatch]::StartNew(); while ($p.playState -ne 3 -and $sw.Elapsed.TotalSeconds -lt 5) { Start-Sleep -Milliseconds 100 }; if ($p.playState -ne 3) { $p.close(); exit 1 }; $sw.Restart(); while ($p.playState -eq 3 -and $sw.Elapsed.TotalMinutes -lt 2) { Start-Sleep -Milliseconds 100 }; if ($p.playState -eq 3) { $p.close(); exit 1 }; $p.close()"
			candidates = append(candidates, struct {
				name string
				args []string
			}{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", player}})
		}
	default:
		if extension == ".mp3" {
			candidates = append(candidates,
				struct {
					name string
					args []string
				}{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}},
				struct {
					name string
					args []string
				}{"mpv", []string{"--no-video", "--really-quiet", path}},
				struct {
					name string
					args []string
				}{"mpg123", []string{"-q", path}},
			)
		} else {
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
				}{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}},
			)
		}
	}
	for _, candidate := range candidates {
		if executable, err := exec.LookPath(candidate.name); err == nil {
			return executable, candidate.args, nil
		}
	}
	return "", nil, fmt.Errorf("no supported sound player is available")
}
