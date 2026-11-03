package sound

import (
	"bytes"
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

func TestPlayerCommandRejectsUnsupportedExtension(t *testing.T) {
	if _, _, err := playerCommand("sound.ogg"); err == nil {
		t.Fatal("playerCommand accepted unsupported sound extension")
	}
}
