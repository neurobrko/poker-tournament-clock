package sound

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBundledDesktopSoundsAreEmbedded(t *testing.T) {
	if len(bundledLevelMP3) < 1000 || len(bundledMinuteWAV) < 1000 {
		t.Fatalf("bundled level MP3/minute WAV too small: %d/%d", len(bundledLevelMP3), len(bundledMinuteWAV))
	}
	if string(bundledLevelMP3[:3]) != "ID3" && bundledLevelMP3[0] != 0xff {
		t.Fatal("bundled level sound is not MP3")
	}
	if string(bundledMinuteWAV[:4]) != "RIFF" || string(bundledMinuteWAV[8:12]) != "WAVE" {
		t.Fatal("bundled minute sound is not WAV")
	}
	if err := PlayMinuteWarningDesktop("", false, nil); err != nil {
		t.Fatalf("disabled minute-warning sound attempted playback: %v", err)
	}
}

func TestPlaybackFailureIsReportedAsynchronously(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses the Linux paplay backend")
	}
	dir := t.TempDir()
	player := filepath.Join(dir, "paplay")
	if err := os.WriteFile(player, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	file := filepath.Join(dir, "sound.wav")
	if err := os.WriteFile(file, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	failed := make(chan error, 1)
	if err := play(file, true, nil, ".wav", func(err error) { failed <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("playback callback returned nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("playback failure was not reported")
	}
}

func TestMP3PlayerCommandDoesNotPassUnsupportedNoStdinFlag(t *testing.T) {
	command, args, err := playerCommand("level-change.mp3")
	if err != nil {
		t.Skipf("no MP3 player installed: %v", err)
	}
	if filepath.Base(command) != "ffplay" {
		return
	}
	for _, argument := range args {
		if argument == "-nostdin" {
			t.Fatal("ffplay does not support -nostdin; MP3 playback would silently fail")
		}
	}
}

func TestPlayerCommandRejectsUnsupportedExtension(t *testing.T) {
	if _, _, err := playerCommand("sound.ogg"); err == nil {
		t.Fatal("playerCommand accepted unsupported sound extension")
	}
}
