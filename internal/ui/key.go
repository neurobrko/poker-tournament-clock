package ui

type keyType uint8

const (
	keyRunes keyType = iota
	keySpace
	keyEsc
	keyEnter
	keyBackspace
	keyUp
	keyDown
	keyHome
	keyEnd
	keyLeft
	keyRight
)

type keyEvent struct {
	Type  keyType
	Runes []rune
	Alt   bool
}

func (key keyEvent) String() string { return string(key.Runes) }
