package tui

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Spacedust colors centralize UI styling so later light and alternate palettes can
// replace these values without changing view components.
var Spacedust = struct {
	Background color.NRGBA
	Panel      color.NRGBA
	Raised     color.NRGBA
	Foreground color.NRGBA
	Muted      color.NRGBA
	Accent     color.NRGBA
	Teal       color.NRGBA
	Green      color.NRGBA
	Yellow     color.NRGBA
	Red        color.NRGBA
	Line       color.NRGBA
}{
	Background: color.NRGBA{R: 0x0a, G: 0x1e, B: 0x24, A: 0xff},
	Panel:      color.NRGBA{R: 0x10, G: 0x2a, B: 0x31, A: 0xff},
	Raised:     color.NRGBA{R: 0x16, G: 0x36, B: 0x3e, A: 0xff},
	Foreground: color.NRGBA{R: 0xec, G: 0xef, B: 0xc1, A: 0xff},
	Muted:      color.NRGBA{R: 0x9a, G: 0xb4, B: 0xb0, A: 0xff},
	Accent:     color.NRGBA{R: 0xff, G: 0x8a, B: 0x39, A: 0xff},
	Teal:       color.NRGBA{R: 0x06, G: 0xaf, B: 0xc7, A: 0xff},
	Green:      color.NRGBA{R: 0x5c, G: 0xab, B: 0x96, A: 0xff},
	Yellow:     color.NRGBA{R: 0xe3, G: 0xcd, B: 0x7b, A: 0xff},
	Red:        color.NRGBA{R: 0xe3, G: 0x5a, B: 0x00, A: 0xff},
	Line:       color.NRGBA{R: 0x3c, G: 0x5d, B: 0x62, A: 0xff},
}

// Ubuntu's UFL license is bundled alongside the static font files.
//
//go:embed fonts/UbuntuSans-Regular.ttf
var ubuntuRegular []byte

//go:embed fonts/UbuntuSans-Bold.ttf
var ubuntuBold []byte

//go:embed fonts/UbuntuSans-Italic.ttf
var ubuntuItalic []byte

//go:embed fonts/UbuntuSans-BoldItalic.ttf
var ubuntuBoldItalic []byte

//go:embed fonts/UbuntuSans-Condensed-Regular.ttf
var ubuntuCondensed []byte

//go:embed fonts/UbuntuSans-Condensed-Bold.ttf
var ubuntuCondensedBold []byte

//go:embed fonts/UbuntuSansMono-Regular.ttf
var ubuntuMono []byte

//go:embed fonts/UbuntuSansMono-Bold.ttf
var ubuntuMonoBold []byte

type spacedustTheme struct {
	base               fyne.Theme
	regular            fyne.Resource
	bold               fyne.Resource
	italic             fyne.Resource
	boldItalic         fyne.Resource
	condensed          fyne.Resource
	condensedBold      fyne.Resource
	monospace          fyne.Resource
	monospaceBold      fyne.Resource
	useCondensedFamily bool
}

func newSpacedustTheme() fyne.Theme { return newUbuntuTheme(false) }
func newCondensedTheme() fyne.Theme { return newUbuntuTheme(true) }

func newUbuntuTheme(condensed bool) fyne.Theme {
	return &spacedustTheme{
		base:               theme.DarkTheme(),
		regular:            fyne.NewStaticResource("UbuntuSans-Regular.ttf", ubuntuRegular),
		bold:               fyne.NewStaticResource("UbuntuSans-Bold.ttf", ubuntuBold),
		italic:             fyne.NewStaticResource("UbuntuSans-Italic.ttf", ubuntuItalic),
		boldItalic:         fyne.NewStaticResource("UbuntuSans-BoldItalic.ttf", ubuntuBoldItalic),
		condensed:          fyne.NewStaticResource("UbuntuSans-Condensed-Regular.ttf", ubuntuCondensed),
		condensedBold:      fyne.NewStaticResource("UbuntuSans-Condensed-Bold.ttf", ubuntuCondensedBold),
		monospace:          fyne.NewStaticResource("UbuntuSansMono-Regular.ttf", ubuntuMono),
		monospaceBold:      fyne.NewStaticResource("UbuntuSansMono-Bold.ttf", ubuntuMonoBold),
		useCondensedFamily: condensed,
	}
}

func (t *spacedustTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return Spacedust.Background
	case theme.ColorNameForeground:
		return Spacedust.Foreground
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		return Spacedust.Accent
	case theme.ColorNameButton:
		return Spacedust.Raised
	case theme.ColorNameDisabled:
		return Spacedust.Muted
	case theme.ColorNameDisabledButton:
		return Spacedust.Panel
	case theme.ColorNameHeaderBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground, theme.ColorNameInputBackground:
		return Spacedust.Panel
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return Spacedust.Line
	case theme.ColorNameShadow:
		return color.NRGBA{A: 0}
	case theme.ColorNamePlaceHolder:
		return Spacedust.Muted
	case theme.ColorNameSuccess:
		return Spacedust.Green
	case theme.ColorNameWarning:
		return Spacedust.Yellow
	case theme.ColorNameError:
		return Spacedust.Red
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess, theme.ColorNameForegroundOnWarning, theme.ColorNameForegroundOnError:
		return Spacedust.Background
	}
	return t.base.Color(name, theme.VariantDark)
}

func (t *spacedustTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Symbol {
		return t.base.Font(style)
	}
	if style.Monospace {
		if style.Bold {
			return t.monospaceBold
		}
		return t.monospace
	}
	if t.useCondensedFamily {
		if style.Bold || style.Italic {
			return t.condensedBold
		}
		return t.condensed
	}
	if style.Bold && style.Italic {
		return t.boldItalic
	}
	if style.Bold {
		return t.bold
	}
	if style.Italic {
		return t.italic
	}
	return t.regular
}
func (t *spacedustTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
func (t *spacedustTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 16
	case theme.SizeNameHeadingText:
		return 25
	case theme.SizeNameSubHeadingText:
		return 21
	case theme.SizeNameCaptionText:
		return 13
	case theme.SizeNamePadding:
		return 9
	case theme.SizeNameInnerPadding:
		return 7
	}
	return t.base.Size(name)
}
