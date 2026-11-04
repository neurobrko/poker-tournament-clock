package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultTemplate(t *testing.T) {
	template, err := DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := template.Purchases.InitialBuyIn, (PurchaseType{Price: 200, Chips: 1000}); got != want {
		t.Fatalf("initial buy-in = %+v, want %+v", got, want)
	}
	if got, want := template.Purchases.Rebuy, (PurchaseType{Price: 200, Chips: 1000}); got != want {
		t.Fatalf("rebuy = %+v, want %+v", got, want)
	}
	if got, want := template.Purchases.AddOn, (PurchaseType{Price: 200, Chips: 5000}); got != want {
		t.Fatalf("add-on = %+v, want %+v", got, want)
	}
	if got, want := len(template.Schedule), 15; got != want {
		t.Fatalf("schedule length = %d, want %d", got, want)
	}
	levels := make([]Segment, 0, 12)
	breaks := 0
	for _, segment := range template.Schedule {
		if segment.Kind == SegmentLevel {
			levels = append(levels, segment)
			if segment.Duration.Value() != 20*time.Minute {
				t.Errorf("level duration = %s, want 20m", segment.Duration.Value())
			}
		} else {
			breaks++
			if segment.Duration.Value() != 10*time.Minute {
				t.Errorf("break duration = %s, want 10m", segment.Duration.Value())
			}
		}
	}
	if len(levels) != 12 || breaks != 3 {
		t.Fatalf("got %d levels and %d breaks, want 12 and 3", len(levels), breaks)
	}
	wantBlinds := [][2]int64{{5, 10}, {10, 20}, {25, 50}, {50, 100}, {100, 200}, {250, 500}, {500, 1000}, {1000, 2000}, {2500, 5000}, {5000, 10000}, {10000, 20000}, {25000, 50000}}
	for i, want := range wantBlinds {
		if got := [2]int64{levels[i].SmallBlind, levels[i].BigBlind}; got != want {
			t.Errorf("level %d blinds = %v, want %v", i+1, got, want)
		}
		if levels[i].Ante.Kind != AnteNone {
			t.Errorf("level %d ante kind = %q, want none", i+1, levels[i].Ante.Kind)
		}
	}
	if template.Registration.RegistrationDeadline != (Deadline{At: DeadlineAtBreakEnd, Target: 1}) {
		t.Errorf("registration deadline = %+v", template.Registration.RegistrationDeadline)
	}
	wantTiers := []PrizeTier{
		{MinPlayers: 2, Percentages: []Percentage{"70", "30"}},
		{MinPlayers: 5, Percentages: []Percentage{"60", "25", "15"}},
		{MinPlayers: 10, Percentages: []Percentage{"40", "30", "20", "10"}},
	}
	if !reflect.DeepEqual(template.PrizeTiers, wantTiers) {
		t.Fatalf("default prize tiers = %+v, want %+v", template.PrizeTiers, wantTiers)
	}
	if err := template.Validate(); err != nil {
		t.Fatalf("default payout tiers are invalid: %v", err)
	}
}

func TestPriceParsingUsesExactDecimalRepresentation(t *testing.T) {
	for _, raw := range []string{"2", "2.0", "2.00"} {
		t.Run(raw, func(t *testing.T) {
			data := replaceFirst(t, "price: 2.00", "price: "+raw)
			template, err := ParseTemplate(data)
			if err != nil {
				t.Fatal(err)
			}
			if template.Purchases.InitialBuyIn.Price != 200 {
				t.Fatalf("price = %d cents, want 200", template.Purchases.InitialBuyIn.Price)
			}
		})
	}
}

func TestPriceParsingRejectsInvalidPrecisionAndAmounts(t *testing.T) {
	for _, raw := range []string{"2.05", "2.001", "2e0", "0.00", "-2.00"} {
		t.Run(raw, func(t *testing.T) {
			data := replaceFirst(t, "price: 2.00", "price: "+raw)
			if _, err := ParseTemplate(data); err == nil {
				t.Fatalf("ParseTemplate accepted price %q", raw)
			}
		})
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	template, err := DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseTemplate(data)
	if err != nil {
		t.Fatalf("parse exported template: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(got, template) {
		t.Fatalf("round trip changed template")
	}
}

func TestTemplateValidationRejectsUnknownFieldsAndVersions(t *testing.T) {
	data, err := DefaultYAML()
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), data...), []byte("unknown_setting: true\n")...)
	if _, err := ParseTemplate(unknown); err == nil {
		t.Fatal("ParseTemplate accepted an unknown field")
	}
	version := replaceFirst(t, "version: 1", "version: 2")
	if _, err := ParseTemplate(version); err == nil {
		t.Fatal("ParseTemplate accepted unsupported version")
	}
}

func TestPrizePercentagesAreValidatedExactly(t *testing.T) {
	template, err := DefaultTemplate()
	if err != nil {
		t.Fatal(err)
	}
	template.PrizeTiers[0].Percentages = []Percentage{"60.00", "25.0", "15.00"}
	encoded, err := MarshalTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTemplate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.PrizeTiers[0].Percentages[0]; got != "60.00" {
		t.Fatalf("percentage representation = %q, want original precision", got)
	}

	invalid := template
	invalid.PrizeTiers = append([]PrizeTier(nil), template.PrizeTiers...)
	invalid.PrizeTiers[0].Percentages = []Percentage{"60", "25", "14"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate accepted percentages that do not sum to 100")
	}
}

func TestRegistrationDeadlineMustReferenceConfiguredSegment(t *testing.T) {
	data := replaceFirst(t, "target: 1", "target: 4")
	if _, err := ParseTemplate(data); err == nil {
		t.Fatal("ParseTemplate accepted a deadline outside the schedule")
	}
}

func replaceFirst(t *testing.T, old, replacement string) []byte {
	t.Helper()
	data, err := DefaultYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("test fixture does not contain %q", old)
	}
	return []byte(strings.Replace(string(data), old, replacement, 1))
}
