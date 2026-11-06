// Package config defines and validates reusable tournament templates.
package config

import (
	"bytes"
	"embed"
	"fmt"
	"image/color"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/colornames"
	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

//go:embed default.yaml
var defaultTemplateFS embed.FS

// EuroCents stores money without floating-point arithmetic.
type EuroCents int64

func (m *EuroCents) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("price must be a decimal euro amount")
	}
	raw := strings.TrimSpace(node.Value)
	if !decimalPattern.MatchString(raw) {
		return fmt.Errorf("invalid euro amount %q: use a plain decimal such as 2.00", raw)
	}
	amount, ok := new(big.Rat).SetString(raw)
	if !ok {
		return fmt.Errorf("invalid euro amount %q", raw)
	}
	cents := new(big.Rat).Mul(amount, big.NewRat(100, 1))
	if !cents.IsInt() {
		return fmt.Errorf("price %q has precision smaller than one cent", raw)
	}
	integerCents := cents.Num()
	if integerCents.Sign() <= 0 {
		return fmt.Errorf("price must be positive")
	}
	if new(big.Int).Mod(integerCents, big.NewInt(10)).Sign() != 0 {
		return fmt.Errorf("price %q must be a multiple of €0.10", raw)
	}
	if !integerCents.IsInt64() {
		return fmt.Errorf("price %q is too large", raw)
	}
	*m = EuroCents(integerCents.Int64())
	return nil
}

func (m EuroCents) MarshalYAML() (any, error) {
	if m < 0 {
		return nil, fmt.Errorf("price cannot be negative")
	}
	return fmt.Sprintf("%d.%02d", int64(m)/100, int64(m)%100), nil
}

func (m EuroCents) String() string {
	return fmt.Sprintf("%d.%02d €", int64(m)/100, int64(m)%100)
}

// Duration parses human-readable Go durations from YAML, such as 20m or 90s.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a string such as 20m or 90s")
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(node.Value))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d Duration) Value() time.Duration      { return time.Duration(d) }

// Percentage retains the original decimal representation and is interpreted exactly.
type Percentage string

func (p *Percentage) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || !decimalPattern.MatchString(strings.TrimSpace(node.Value)) {
		return fmt.Errorf("percentage must be a plain decimal number")
	}
	*p = Percentage(strings.TrimSpace(node.Value))
	return nil
}

func (p Percentage) MarshalYAML() (any, error) { return string(p), nil }

func (p Percentage) rational() (*big.Rat, error) {
	if !decimalPattern.MatchString(string(p)) {
		return nil, fmt.Errorf("invalid percentage %q", p)
	}
	r, ok := new(big.Rat).SetString(string(p))
	if !ok {
		return nil, fmt.Errorf("invalid percentage %q", p)
	}
	return r, nil
}

type Template struct {
	Version      int                `yaml:"version"`
	Purchases    PurchaseConfig     `yaml:"purchases"`
	Registration RegistrationConfig `yaml:"registration"`
	ChipValues   []ChipValue        `yaml:"chip_values,omitempty"`
	Schedule     []Segment          `yaml:"schedule"`
	PrizeTiers   []PrizeTier        `yaml:"prize_tiers"`
}

type ChipValue struct {
	Value int64  `yaml:"value"`
	Color string `yaml:"color"`
}

type PurchaseConfig struct {
	InitialBuyIn PurchaseType `yaml:"initial_buy_in"`
	Rebuy        PurchaseType `yaml:"rebuy"`
	AddOn        PurchaseType `yaml:"add_on"`
}

type PurchaseType struct {
	Price EuroCents `yaml:"price"`
	Chips int64     `yaml:"chips"`
}

type RegistrationConfig struct {
	RebuysEnabled        bool     `yaml:"rebuys_enabled"`
	RegistrationDeadline Deadline `yaml:"registration_deadline"`
	AddOnsEnabled        bool     `yaml:"add_ons_enabled"`
	AddOnBreak           int      `yaml:"add_on_break,omitempty"`
}

type DeadlineAt string

const (
	DeadlineAtLevelStart DeadlineAt = "start_of_level"
	DeadlineAtBreakStart DeadlineAt = "start_of_break"
	DeadlineAtBreakEnd   DeadlineAt = "end_of_break"
)

type Deadline struct {
	At     DeadlineAt `yaml:"at"`
	Target int        `yaml:"target"`
}

type SegmentKind string

const (
	SegmentLevel SegmentKind = "level"
	SegmentBreak SegmentKind = "break"
)

type Segment struct {
	Kind       SegmentKind `yaml:"kind"`
	Duration   Duration    `yaml:"duration"`
	Label      string      `yaml:"label,omitempty"`
	SmallBlind int64       `yaml:"small_blind,omitempty"`
	BigBlind   int64       `yaml:"big_blind,omitempty"`
	Ante       AnteConfig  `yaml:"ante,omitempty"`
	Reminder   string      `yaml:"reminder,omitempty"`
	ChipUp     int64       `yaml:"chipup,omitempty" json:"chipup,omitempty"`
}

type AnteKind string

const (
	AnteNone      AnteKind = "none"
	AntePerPlayer AnteKind = "per_player"
	AnteBigBlind  AnteKind = "big_blind"
)

type AnteConfig struct {
	Kind   AnteKind `yaml:"kind"`
	Amount int64    `yaml:"amount,omitempty"`
}

type PrizeTier struct {
	MinPlayers  int          `yaml:"min_players"`
	Percentages []Percentage `yaml:"percentages"`
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// ParseTemplate decodes one versioned YAML document and rejects unknown fields.
func ParseTemplate(data []byte) (Template, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var template Template
	if err := decoder.Decode(&template); err != nil {
		return Template{}, fmt.Errorf("decode tournament template: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Template{}, fmt.Errorf("template must contain exactly one YAML document")
		}
		return Template{}, fmt.Errorf("decode trailing YAML document: %w", err)
	}
	if err := template.Validate(); err != nil {
		return Template{}, err
	}
	return template, nil
}

// DefaultTemplate returns a validated copy of the embedded tournament template.
func DefaultTemplate() (Template, error) {
	data, err := defaultTemplateFS.ReadFile("default.yaml")
	if err != nil {
		return Template{}, fmt.Errorf("read embedded default template: %w", err)
	}
	return ParseTemplate(data)
}

// DefaultYAML returns a copy of the embedded default template bytes.
func DefaultYAML() ([]byte, error) {
	data, err := defaultTemplateFS.ReadFile("default.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded default template: %w", err)
	}
	return bytes.Clone(data), nil
}

// MarshalTemplate validates and encodes a template for reusable YAML export.
func MarshalTemplate(template Template) ([]byte, error) {
	if err := template.Validate(); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(template)
	if err != nil {
		return nil, fmt.Errorf("encode tournament template: %w", err)
	}
	return data, nil
}

func (t Template) Validate() error {
	if t.Version != CurrentVersion {
		return fmt.Errorf("unsupported template version %d (supported: %d)", t.Version, CurrentVersion)
	}
	if err := validateChipValues(t.ChipValues); err != nil {
		return err
	}
	if err := validatePurchase("initial buy-in", t.Purchases.InitialBuyIn); err != nil {
		return err
	}
	if err := validatePurchase("rebuy", t.Purchases.Rebuy); err != nil {
		return err
	}
	if err := validatePurchase("add-on", t.Purchases.AddOn); err != nil {
		return err
	}
	if err := t.validateSchedule(); err != nil {
		return err
	}
	if err := t.validateRegistration(); err != nil {
		return err
	}
	return t.validatePrizeTiers()
}

func validateChipValues(chips []ChipValue) error {
	seen := make(map[int64]bool, len(chips))
	for index, chip := range chips {
		if chip.Value <= 0 {
			return fmt.Errorf("chip_values item %d value must be positive", index+1)
		}
		if seen[chip.Value] {
			return fmt.Errorf("chip_values value %d is duplicated", chip.Value)
		}
		seen[chip.Value] = true
		if _, err := ParseChipColor(chip.Color); err != nil {
			return fmt.Errorf("chip_values item %d color: %w", index+1, err)
		}
	}
	return nil
}

// ParseChipColor accepts SVG/HTML color names and CSS-style hexadecimal colors.
func ParseChipColor(value string) (color.NRGBA, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if named, ok := colornames.Map[value]; ok {
		return color.NRGBAModel.Convert(named).(color.NRGBA), nil
	}
	if value == "transparent" {
		return color.NRGBA{}, nil
	}
	if !strings.HasPrefix(value, "#") {
		return color.NRGBA{}, fmt.Errorf("%q is not an HTML color name or hex color", value)
	}
	digits := value[1:]
	var channels []uint8
	switch len(digits) {
	case 3, 4:
		for _, digit := range digits {
			component, err := strconv.ParseUint(string(digit)+string(digit), 16, 8)
			if err != nil {
				return color.NRGBA{}, fmt.Errorf("invalid hex color %q", value)
			}
			channels = append(channels, uint8(component))
		}
	case 6, 8:
		for index := 0; index < len(digits); index += 2 {
			component, err := strconv.ParseUint(digits[index:index+2], 16, 8)
			if err != nil {
				return color.NRGBA{}, fmt.Errorf("invalid hex color %q", value)
			}
			channels = append(channels, uint8(component))
		}
	default:
		return color.NRGBA{}, fmt.Errorf("invalid hex color %q", value)
	}
	result := color.NRGBA{R: channels[0], G: channels[1], B: channels[2], A: 0xff}
	if len(channels) == 4 {
		result.A = channels[3]
	}
	return result, nil
}

func validatePurchase(name string, purchase PurchaseType) error {
	if purchase.Price <= 0 || int64(purchase.Price)%10 != 0 {
		return fmt.Errorf("%s price must be positive and a multiple of €0.10", name)
	}
	if purchase.Chips <= 0 {
		return fmt.Errorf("%s chip amount must be positive", name)
	}
	return nil
}

func (t Template) validateSchedule() error {
	if len(t.Schedule) == 0 {
		return fmt.Errorf("schedule must contain at least one blind level")
	}
	levels, breaks := 0, 0
	previous := SegmentKind("")
	for i, segment := range t.Schedule {
		where := fmt.Sprintf("schedule item %d", i+1)
		if segment.Duration.Value() <= 0 {
			return fmt.Errorf("%s duration must be positive", where)
		}
		switch segment.Kind {
		case SegmentLevel:
			levels++
			if segment.ChipUp != 0 {
				return fmt.Errorf("%s cannot define chipup; chipup is only valid on breaks", where)
			}
			if segment.SmallBlind <= 0 || segment.BigBlind <= segment.SmallBlind {
				return fmt.Errorf("%s requires positive blinds with big blind greater than small blind", where)
			}
			if err := validateAnte(segment.Ante, where); err != nil {
				return err
			}
		case SegmentBreak:
			breaks++
			if segment.ChipUp < 0 {
				return fmt.Errorf("%s chipup must be positive", where)
			}
			if segment.ChipUp > 0 && len(t.ChipValues) > 0 {
				found := false
				for _, chip := range t.ChipValues {
					found = found || chip.Value == segment.ChipUp
				}
				if !found {
					return fmt.Errorf("%s chipup %d is not in chip_values", where, segment.ChipUp)
				}
			}
			if i == 0 || previous == SegmentBreak {
				return fmt.Errorf("%s must follow a blind level", where)
			}
			if segment.SmallBlind != 0 || segment.BigBlind != 0 || (segment.Ante.Kind != "" && segment.Ante.Kind != AnteNone) || segment.Ante.Amount != 0 {
				return fmt.Errorf("%s cannot define blinds or an ante", where)
			}
		default:
			return fmt.Errorf("%s has unknown kind %q", where, segment.Kind)
		}
		previous = segment.Kind
	}
	if levels == 0 {
		return fmt.Errorf("schedule must contain at least one blind level")
	}
	if previous == SegmentBreak {
		return fmt.Errorf("schedule cannot end with a break")
	}
	return nil
}

func validateAnte(ante AnteConfig, where string) error {
	switch ante.Kind {
	case "", AnteNone:
		if ante.Amount != 0 {
			return fmt.Errorf("%s no-ante configuration must have amount zero", where)
		}
	case AntePerPlayer, AnteBigBlind:
		if ante.Amount <= 0 {
			return fmt.Errorf("%s ante amount must be positive", where)
		}
	default:
		return fmt.Errorf("%s has unknown ante kind %q", where, ante.Kind)
	}
	return nil
}

func (t Template) validateRegistration() error {
	d := t.Registration.RegistrationDeadline
	var expected SegmentKind
	switch d.At {
	case DeadlineAtLevelStart:
		expected = SegmentLevel
	case DeadlineAtBreakStart, DeadlineAtBreakEnd:
		expected = SegmentBreak
	default:
		return fmt.Errorf("registration deadline has unknown position %q", d.At)
	}
	if d.Target < 1 || countKind(t.Schedule, expected) < d.Target {
		return fmt.Errorf("registration deadline target %d does not identify a configured %s", d.Target, expected)
	}
	if t.Registration.AddOnsEnabled {
		if t.Registration.AddOnBreak < 1 || countKind(t.Schedule, SegmentBreak) < t.Registration.AddOnBreak {
			return fmt.Errorf("add-on break %d does not identify a configured break", t.Registration.AddOnBreak)
		}
	} else if t.Registration.AddOnBreak < 0 {
		return fmt.Errorf("add-on break cannot be negative")
	}
	return nil
}

func (t Template) validatePrizeTiers() error {
	if len(t.PrizeTiers) == 0 {
		return fmt.Errorf("at least one prize tier is required")
	}
	if t.PrizeTiers[0].MinPlayers != 2 {
		return fmt.Errorf("first prize tier must begin at 2 players")
	}
	previousThreshold := 1
	for i, tier := range t.PrizeTiers {
		if tier.MinPlayers <= previousThreshold {
			return fmt.Errorf("prize tier thresholds must be strictly increasing")
		}
		if len(tier.Percentages) == 0 {
			return fmt.Errorf("prize tier %d must contain at least one place", i+1)
		}
		total := new(big.Rat)
		var previous *big.Rat
		for place, percentage := range tier.Percentages {
			value, err := percentage.rational()
			if err != nil {
				return fmt.Errorf("prize tier %d place %d: %w", i+1, place+1, err)
			}
			if value.Sign() <= 0 {
				return fmt.Errorf("prize tier %d percentages must be positive", i+1)
			}
			if previous != nil && value.Cmp(previous) > 0 {
				return fmt.Errorf("prize tier %d percentages must be non-increasing by place", i+1)
			}
			total.Add(total, value)
			previous = value
		}
		if total.Cmp(big.NewRat(100, 1)) != 0 {
			return fmt.Errorf("prize tier %d percentages must sum exactly to 100%%", i+1)
		}
		previousThreshold = tier.MinPlayers
	}
	return nil
}

func countKind(schedule []Segment, kind SegmentKind) int {
	count := 0
	for _, segment := range schedule {
		if segment.Kind == kind {
			count++
		}
	}
	return count
}
