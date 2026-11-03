// Package preferences stores machine-local settings that are not part of tournament configuration.
package preferences

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Preferences struct {
	SoundEnabled bool   `json:"sound_enabled"`
	SoundFile    string `json:"sound_file,omitempty"`
}

func Default() Preferences { return Preferences{SoundEnabled: true} }

func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(base, "poker-clock", "preferences.json"), nil
}

func Load() (Preferences, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Default(), fmt.Errorf("read preferences: %w", err)
	}
	prefs := Default()
	if err := json.Unmarshal(data, &prefs); err != nil {
		return Default(), fmt.Errorf("decode preferences: %w", err)
	}
	if strings.TrimSpace(prefs.SoundFile) != "" {
		prefs.SoundFile = filepath.Clean(prefs.SoundFile)
	}
	return prefs, nil
}

func Save(prefs Preferences) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if prefs.SoundFile != "" {
		ext := strings.ToLower(filepath.Ext(prefs.SoundFile))
		if ext != ".wav" && ext != ".mp3" {
			return fmt.Errorf("sound file must be WAV or MP3")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".preferences-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary preferences: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect temporary preferences: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace preferences: %w", err)
	}
	return nil
}
