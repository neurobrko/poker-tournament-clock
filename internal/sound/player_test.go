package sound

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedWAVAndDisabledPlayback(t *testing.T) {
	if !bytes.HasPrefix(bundledWAV, []byte("RIFF")) || !bytes.Contains(bundledWAV, []byte("WAVE")) {
		t.Fatal("embedded notification is not a WAV file")
	}
	if err := Play("", false); err != nil {
		t.Fatalf("disabled sound attempted playback: %v", err)
	}
}

func TestPlayDesktopDoesNotWriteTerminalBell(t *testing.T) {
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = original })
	if err := PlayDesktop("notification.ogg", true); err == nil {
		t.Fatal("unsupported desktop sound unexpectedly succeeded")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if len(output) != 0 {
		t.Fatalf("GUI sound failure wrote to terminal stderr: %q", output)
	}
}

func TestBundledDesktopMP3SoundsAreEmbedded(t *testing.T) {
	if len(bundledLevelMP3) < 1000 || len(bundledMinuteMP3) < 1000 {
		t.Fatalf("bundled level/minute MP3 too small: %d/%d", len(bundledLevelMP3), len(bundledMinuteMP3))
	}
	if err := PlayMinuteWarningDesktop("", false); err != nil {
		t.Fatalf("disabled minute-warning sound attempted playback: %v", err)
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
