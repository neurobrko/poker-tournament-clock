package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLoadAndAtomicSave(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	prefs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.SoundEnabled || prefs.SoundFile != "" {
		t.Fatalf("default preferences = %+v", prefs)
	}
	prefs.SoundEnabled = false
	prefs.SoundFile = filepath.Join(root, "level-change.wav")
	prefs.MinuteSoundFile = filepath.Join(root, "minute-warning.mp3")
	if err := Save(prefs); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != prefs {
		t.Fatalf("loaded preferences = %+v, want %+v", loaded, prefs)
	}
	info, err := os.Stat(pathMust(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("preferences file mode = %o, want private", info.Mode().Perm())
	}
}

func TestSaveRejectsUnsupportedSoundExtension(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Preferences{SoundEnabled: true, SoundFile: "sound.ogg"}); err == nil {
		t.Fatal("Save accepted unsupported sound extension")
	}
	if err := Save(Preferences{SoundEnabled: true, MinuteSoundFile: "sound.ogg"}); err == nil {
		t.Fatal("Save accepted unsupported minute-warning sound extension")
	}
}

func pathMust(t *testing.T) string {
	t.Helper()
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}
