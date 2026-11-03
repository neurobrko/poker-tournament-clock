//go:build linux || darwin

package sleepinhibit

import "testing"

func TestParseX11Settings(t *testing.T) {
	output := `Screen Saver:
  prefer blanking: yes    allow exposures: no
  timeout: 600    cycle: 600
DPMS (Energy Star):
  Standby: 600    Suspend: 900    Off: 1200
  DPMS is Enabled`
	settings, err := parseX11Settings(output)
	if err != nil {
		t.Fatal(err)
	}
	if settings.blanking != "yes" || settings.exposures != "no" || settings.timeout != 600 || settings.cycle != 600 || settings.standby != 600 || settings.suspend != 900 || settings.off != 1200 || !settings.dpmsEnabled {
		t.Fatalf("parsed X11 settings = %+v", settings)
	}
}

func TestParseX11SettingsRejectsIncompleteOutput(t *testing.T) {
	if _, err := parseX11Settings("unrecognized output"); err == nil {
		t.Fatal("parseX11Settings accepted invalid output")
	}
}
