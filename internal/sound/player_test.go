package sound

import (
	"bytes"
	"io"
	"os"
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

func TestPlayerCommandRejectsUnsupportedExtension(t *testing.T) {
	if _, _, err := playerCommand("sound.ogg"); err == nil {
		t.Fatal("playerCommand accepted unsupported sound extension")
	}
}
