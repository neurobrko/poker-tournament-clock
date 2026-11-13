package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/poker"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

const maxResolverPlayers = 23

var (
	resolverSlotSize       = fyne.NewSize(44, 66)
	resolverDeckSize       = resolverSlotSize
	resolverResultCardSize = fyne.NewSize(32, 48)
)

type resolverSlotGroup uint8

const (
	resolverCommunity resolverSlotGroup = iota
	resolverPlayer
)

type resolverSlot struct {
	group  resolverSlotGroup
	player int
	index  int
	valid  bool
}

type resolverPlayerState struct {
	name string
	hole [2]*poker.Card
}

type resolverResult struct {
	player int
	place  int
	hand   poker.HandValue
}

type handResolver struct {
	origin      screen
	community   *[5]*poker.Card
	players     []resolverPlayerState
	active      resolverSlot
	last        resolverSlot
	pendingRank uint8
	hasRank     bool
	results     []resolverResult
}

func (m *Model) openHandResolver(origin screen) {
	var players []resolverPlayerState
	if origin == screenMain {
		for _, player := range activePlayers(m.players) {
			if len(players) == maxResolverPlayers {
				break
			}
			players = append(players, resolverPlayerState{name: player.Name})
		}
	}
	for len(players) < 2 {
		players = append(players, resolverPlayerState{name: fmt.Sprintf("Player %d", len(players)+1)})
	}
	m.screen = screenResolver
	m.resolver = &handResolver{origin: origin, community: &[5]*poker.Card{}, players: players, active: resolverSlot{group: resolverCommunity, index: 0, valid: true}}
	m.statusMessage, m.statusIsError = "", false
	if origin == screenMain && len(activePlayers(m.players)) > maxResolverPlayers {
		m.setStatus("Only 23 active players fit in a standard deck.", true)
	}
}

func (m *Model) requestResolverPlayerNameEdit() {
	r := m.resolver
	if r == nil {
		return
	}
	choices := make([]string, len(r.players))
	for i, player := range r.players {
		name := strings.TrimSpace(player.name)
		if name == "" {
			name = fmt.Sprintf("Player %d", i+1)
		}
		choices[i] = fmt.Sprintf("%d · %s", i+1, name)
	}
	m.modal = &dialog{kind: dialogMenu, purpose: menuResolverEditPlayer, title: "Choose player to rename", choices: choices}
}

func (m *Model) editResolverPlayerName(index int) {
	if m.resolver == nil || index < 0 || index >= len(m.resolver.players) {
		return
	}
	name := m.resolver.players[index].name
	m.modal = &dialog{kind: dialogText, title: "Player name", prompt: "Optional name (blank restores numbered label)", input: name, textPurpose: textResolverPlayerName, resolverPlayer: index}
}

func (m *Model) resetResolverCards() {
	r := m.resolver
	if r == nil {
		return
	}
	if r.community != nil {
		for i := range r.community {
			r.community[i] = nil
		}
	}
	for i := range r.players {
		r.players[i].hole = [2]*poker.Card{}
	}
	r.active = resolverSlot{group: resolverCommunity, index: 0, valid: true}
	r.last, r.results = resolverSlot{}, nil
	r.hasRank = false
	m.statusMessage, m.statusIsError = "", false
}

func (m *Model) closeHandResolver() {
	if m.resolver != nil {
		m.screen = m.resolver.origin
	}
	m.resolver = nil
	m.statusMessage, m.statusIsError = "", false
}

func (m *Model) handleHandResolverKey(key keyEvent) bool {
	r := m.resolver
	if r == nil {
		return false
	}
	switch key.Type {
	case keyEsc:
		m.closeHandResolver()
	case keyEnter:
		m.resolveHands()
	case keyBackspace:
		m.undoResolverCard()
	case keyTab, keyDown, keyRight:
		r.moveActive(1)
	case keyUp, keyLeft:
		r.moveActive(-1)
	case keyRunes:
		if len(key.Runes) != 1 {
			return false
		}
		value := key.Runes[0]
		switch value {
		case '+':
			m.addResolverPlayer()
		case '-':
			m.requestResolverPlayerRemoval()
		case 'e', 'E':
			m.requestResolverPlayerNameEdit()
		case 'r', 'R':
			m.resetResolverCards()
		default:
			if rank, ok := resolverRank(value); ok {
				r.pendingRank, r.hasRank = rank, true
				m.setStatus(fmt.Sprintf("Rank %s selected; press suit H, C, S, or D.", poker.RankName(rank)), false)
			} else if suit, ok := resolverSuit(value); ok {
				if !r.hasRank {
					m.setStatus("Select rank first, then suit H, C, S, or D.", true)
					return false
				}
				m.selectResolverCard(poker.Card{Rank: r.pendingRank, Suit: suit})
				r.hasRank = false
			} else {
				return false
			}
		}
	}
	return false
}

func resolverSuit(value rune) (poker.Suit, bool) {
	switch strings.ToUpper(string(value)) {
	case "C":
		return poker.Clubs, true
	case "D":
		return poker.Diamonds, true
	case "H":
		return poker.Hearts, true
	case "S":
		return poker.Spades, true
	default:
		return 0, false
	}
}

func resolverRank(value rune) (uint8, bool) {
	switch strings.ToUpper(string(value)) {
	case "0", "1":
		return 10, true
	case "J":
		return 11, true
	case "Q":
		return 12, true
	case "K":
		return 13, true
	case "A":
		return 14, true
	default:
		if value >= '2' && value <= '9' {
			return uint8(value - '0'), true
		}
		return 0, false
	}
}

func (m *Model) addResolverPlayer() {
	r := m.resolver
	if r == nil || len(r.players) >= maxResolverPlayers {
		return
	}
	name := ""
	for n := 1; name == ""; n++ {
		candidate := fmt.Sprintf("Player %d", n)
		for _, player := range r.players {
			if player.name == candidate {
				candidate = ""
				break
			}
		}
		name = candidate
	}
	index := len(r.players)
	r.players = append(r.players, resolverPlayerState{name: name})
	r.active = resolverSlot{group: resolverPlayer, player: index, index: 0, valid: true}
	r.hasRank, r.results = false, nil
	m.statusMessage, m.statusIsError = "", false
}

func (m *Model) requestResolverPlayerRemoval() {
	r := m.resolver
	if r == nil || len(r.players) <= 2 {
		return
	}
	players := make([]tournament.Player, len(r.players))
	for i, player := range r.players {
		players[i] = tournament.Player{ID: strconv.Itoa(i), Name: player.name}
	}
	m.modal = &dialog{kind: dialogMultiPlayers, purpose: selectResolverRemovePlayer, title: "Choose players to remove · Space to select", players: players, selected: make(map[string]bool)}
}

func (m *Model) removeResolverPlayer(index int) {
	r := m.resolver
	if r == nil || len(r.players) <= 2 || index < 0 || index >= len(r.players) {
		return
	}
	r.players = append(r.players[:index], r.players[index+1:]...)
	if r.last.group == resolverPlayer {
		switch {
		case r.last.player == index:
			r.last = resolverSlot{}
		case r.last.player > index:
			r.last.player--
		}
	}
	if r.active.group == resolverPlayer {
		switch {
		case r.active.player == index:
			r.active.player = min(index, len(r.players)-1)
		case r.active.player > index:
			r.active.player--
		}
	}
	r.results = nil
	m.statusMessage, m.statusIsError = "", false
}

func (m *Model) setResolverActive(slot resolverSlot) {
	if m.resolver == nil || !m.resolver.slotAllowed(slot) {
		return
	}
	m.resolver.active = slot
	m.resolver.hasRank = false
}

func (m *Model) selectResolverCard(card poker.Card) {
	r := m.resolver
	if r == nil {
		return
	}
	slot := r.active
	if !r.slotAllowed(slot) {
		slot = r.nextEmpty(resolverSlot{})
	}
	if !r.slotAllowed(slot) {
		m.setStatus("Select an empty card slot first.", true)
		return
	}
	if old := r.cardAt(slot); old != nil && *old == card {
		r.last, r.active = slot, slot
		m.statusMessage, m.statusIsError = "", false
		return
	}
	if r.cardUsed(card, slot) {
		m.setStatus("That card is already selected.", true)
		return
	}
	copyCard := card
	r.setCard(slot, &copyCard)
	r.last = slot
	next := r.nextEmpty(slot)
	if next.valid {
		r.active = next
	}
	r.hasRank, r.results = false, nil
	m.statusMessage, m.statusIsError = "", false
}

func (m *Model) undoResolverCard() {
	r := m.resolver
	if r == nil {
		return
	}
	slot := r.active
	if !r.slotAllowed(slot) || r.cardAt(slot) == nil {
		slot = r.last
	}
	if !r.slotAllowed(slot) || r.cardAt(slot) == nil {
		return
	}
	r.setCard(slot, nil)
	r.active, r.last = slot, resolverSlot{}
	r.hasRank, r.results = false, nil
	m.statusMessage, m.statusIsError = "", false
}

func (r *handResolver) slotAllowed(slot resolverSlot) bool {
	if !slot.valid {
		return false
	}
	if slot.group == resolverCommunity {
		return r.community != nil && slot.index >= 0 && slot.index < len(r.community)
	}
	return r.community != nil && slot.player >= 0 && slot.player < len(r.players) && slot.index >= 0 && slot.index < 2
}

func (r *handResolver) cardAt(slot resolverSlot) *poker.Card {
	if !r.slotAllowed(slot) {
		return nil
	}
	if slot.group == resolverCommunity {
		return r.community[slot.index]
	}
	return r.players[slot.player].hole[slot.index]
}

func (r *handResolver) setCard(slot resolverSlot, card *poker.Card) {
	if slot.group == resolverCommunity {
		r.community[slot.index] = card
	} else {
		r.players[slot.player].hole[slot.index] = card
	}
}

func (r *handResolver) cardUsed(card poker.Card, except resolverSlot) bool {
	if r.community != nil {
		for i, selected := range r.community {
			if selected != nil && *selected == card && !(except.valid && except.group == resolverCommunity && except.index == i) {
				return true
			}
		}
	}
	for playerIndex := range r.players {
		for holeIndex, selected := range r.players[playerIndex].hole {
			if selected != nil && *selected == card && !(except.valid && except.group == resolverPlayer && except.player == playerIndex && except.index == holeIndex) {
				return true
			}
		}
	}
	return false
}

func (r *handResolver) ready() bool {
	if !r.communityComplete() || len(r.players) < 2 {
		return false
	}
	for _, player := range r.players {
		if player.hole[0] == nil || player.hole[1] == nil {
			return false
		}
	}
	return true
}

func (r *handResolver) communityComplete() bool {
	if r.community == nil {
		return false
	}
	for _, card := range r.community {
		if card == nil {
			return false
		}
	}
	return true
}

func (r *handResolver) nextEmpty(after resolverSlot) resolverSlot {
	if after.group == resolverPlayer {
		if next := r.nextEmptyPlayer(after); next.valid {
			return next
		}
	}
	if r.community != nil {
		for i, card := range r.community {
			if card == nil && !(after.group == resolverCommunity && after.index >= i) {
				return resolverSlot{group: resolverCommunity, index: i, valid: true}
			}
		}
		for i, card := range r.community {
			if card == nil {
				return resolverSlot{group: resolverCommunity, index: i, valid: true}
			}
		}
	}
	if r.communityComplete() {
		return r.nextEmptyPlayer(resolverSlot{})
	}
	return resolverSlot{}
}

func (r *handResolver) nextEmptyPlayer(after resolverSlot) resolverSlot {
	start := 0
	if after.group == resolverPlayer {
		start = after.player*2 + after.index + 1
	}
	for offset := 0; offset < len(r.players)*2; offset++ {
		flat := (start + offset) % (len(r.players) * 2)
		player, hole := flat/2, flat%2
		if r.players[player].hole[hole] == nil {
			return resolverSlot{group: resolverPlayer, player: player, index: hole, valid: true}
		}
	}
	return resolverSlot{}
}

func (r *handResolver) moveActive(delta int) {
	var slots []resolverSlot
	if r.community != nil {
		for i := range r.community {
			slots = append(slots, resolverSlot{group: resolverCommunity, index: i, valid: true})
		}
	}
	if r.community != nil {
		for player := range r.players {
			for hole := 0; hole < 2; hole++ {
				slots = append(slots, resolverSlot{group: resolverPlayer, player: player, index: hole, valid: true})
			}
		}
	}
	if len(slots) == 0 {
		return
	}
	index := -1
	for i, slot := range slots {
		if sameResolverSlot(slot, r.active) {
			index = i
			break
		}
	}
	if index < 0 {
		if delta < 0 {
			index = 0
		} else {
			index = -1
		}
	}
	index = (index + delta + len(slots)) % len(slots)
	r.active, r.hasRank = slots[index], false
}

func sameResolverSlot(a, b resolverSlot) bool {
	return a.valid && b.valid && a.group == b.group && a.player == b.player && a.index == b.index
}

func (m *Model) resolveHands() {
	r := m.resolver
	if r == nil || !r.communityComplete() {
		m.setStatus("Resolve requires all five community cards.", true)
		return
	}
	for _, player := range r.players {
		if player.hole[0] == nil || player.hole[1] == nil {
			m.setStatus("Each player needs two hole cards.", true)
			return
		}
	}
	results := make([]resolverResult, 0, len(r.players))
	for index, player := range r.players {
		cards := make([]poker.Card, 0, 7)
		for _, card := range r.community {
			cards = append(cards, *card)
		}
		cards = append(cards, *player.hole[0], *player.hole[1])
		hand, err := poker.EvaluateBest(cards)
		if err != nil {
			m.setStatus(err, true)
			return
		}
		results = append(results, resolverResult{player: index, hand: hand})
	}
	sort.SliceStable(results, func(i, j int) bool { return poker.Compare(results[i].hand, results[j].hand) > 0 })
	place := 0
	for i := range results {
		if i == 0 || poker.Compare(results[i-1].hand, results[i].hand) != 0 {
			place = i + 1
		}
		results[i].place = place
	}
	r.results = results
	m.statusMessage, m.statusIsError = "Hands resolved.", false
}

func suitName(suit poker.Suit) string {
	return map[poker.Suit]string{poker.Clubs: "♣", poker.Diamonds: "♦", poker.Hearts: "♥", poker.Spades: "♠"}[suit]
}

func suitLetter(suit poker.Suit) string {
	return map[poker.Suit]string{poker.Hearts: "H", poker.Clubs: "C", poker.Spades: "S", poker.Diamonds: "D"}[suit]
}

var (
	resolverCardFace         = color.NRGBA{R: 0xf4, G: 0xf1, B: 0xe4, A: 0xff}
	resolverDisabledCardFace = color.NRGBA{R: 0xbb, G: 0xbe, B: 0xb8, A: 0xff}
	resolverBlackSuit        = color.NRGBA{R: 0x12, G: 0x18, B: 0x1a, A: 0xff}
	resolverRedSuit          = color.NRGBA{R: 0xc2, G: 0x18, B: 0x2b, A: 0xff}
)

func resolverSuitColor(suit poker.Suit) color.Color {
	if suit == poker.Hearts || suit == poker.Diamonds {
		return resolverRedSuit
	}
	return resolverBlackSuit
}

func (d *desktop) handResolverView() fyne.CanvasObject {
	r := d.model.resolver
	title := sectionLabel("HAND RESOLVER")
	communityTitle := widget.NewLabel("Community board · five cards")
	communityTitle.TextStyle = fyne.TextStyle{Bold: true}
	boardCards := container.NewHBox()
	for index, card := range r.community {
		slot := resolverSlot{group: resolverCommunity, index: index, valid: true}
		boardCards.Add(d.resolverSlotButton(r, slot, card))
	}

	playersTitle := widget.NewLabel("Players")
	playersTitle.TextStyle = fyne.TextStyle{Bold: true}
	addPlayer := d.newButton("+", func() { d.model.addResolverPlayer(); d.render(true) })
	removePlayer := d.newButton("−", func() { d.model.requestResolverPlayerRemoval(); d.render(true) })
	editPlayer := d.newButton("Edit name · E", func() { d.model.requestResolverPlayerNameEdit(); d.render(true) })
	if len(r.players) >= maxResolverPlayers {
		addPlayer.Disable()
	}
	if len(r.players) <= 2 {
		removePlayer.Disable()
	}
	playerControls := container.NewHBox(playersTitle, resolverButtonCell(addPlayer, 40), resolverButtonCell(removePlayer, 40), resolverButtonCell(editPlayer, 145))
	playerRows := container.New(layout.NewCustomPaddedVBoxLayout(4))
	for playerIndex, player := range r.players {
		name := strings.TrimSpace(player.name)
		if name == "" {
			name = fmt.Sprintf("Player %d", playerIndex+1)
		}
		nameLabel := widget.NewLabel(resolverNameText(name))
		nameLabel.Wrapping = fyne.TextWrapOff
		nameLabel.Truncation = fyne.TextTruncateEllipsis
		nameCell := container.NewGridWrap(fyne.NewSize(resolverNameColumnWidth(), resolverSlotSize.Height), nameLabel)
		first := d.resolverSlotButton(r, resolverSlot{group: resolverPlayer, player: playerIndex, index: 0, valid: true}, player.hole[0])
		second := d.resolverSlotButton(r, resolverSlot{group: resolverPlayer, player: playerIndex, index: 1, valid: true}, player.hole[1])
		editName := d.newButton("Edit name", func() { d.model.editResolverPlayerName(playerIndex); d.render(true) })
		remove := d.newButton("−", func() { d.model.removeResolverPlayer(playerIndex); d.render(true) })
		if len(r.players) <= 2 {
			remove.Disable()
		}
		playerRows.Add(container.NewHBox(nameCell, first, second, container.NewCenter(resolverButtonCell(editName, 100)), container.NewCenter(resolverButtonCell(remove, 40))))
	}

	playerGap := container.New(&minimumHeightLayout{height: 8}, canvas.NewRectangle(Spacedust.Background))
	sections := container.New(layout.NewCustomPaddedVBoxLayout(4), communityTitle, boardCards, widget.NewSeparator(), playerControls, playerGap, playerRows)
	left := container.NewVScroll(sections)
	deck := d.resolverDeck(r)
	resolve := d.newButton("Resolve  ·  Enter", func() { d.model.resolveHands(); d.render(true) })
	resolve.Importance = widget.HighImportance
	if !r.ready() {
		resolve.Disable()
	}
	back := d.newButton("Back  ·  Esc", func() { d.model.closeHandResolver(); d.render(true) })
	reset := d.newButton("Reset deck  ·  R", func() { d.model.resetResolverCards(); d.render(true) })
	footer := container.NewVBox(container.NewHBox(resolverButtonCell(resolve, 140), resolverButtonCell(back, 100), resolverButtonCell(reset, 150)), d.statusNotice())
	deckLegend := mutedLabel("Card keys: rank first (J/K/Q/A, 2–9, 0/1=10), then suit H/C/S/D.")
	deckColumn := container.New(layout.NewCustomPaddedVBoxLayout(6), container.NewHScroll(deck), deckLegend)
	results := d.handResolverResults(r)
	right := container.New(&resolverDeckResultsLayout{}, deckColumn, results)
	content := container.New(&resolverColumnsLayout{}, left, right)
	return container.NewBorder(title, footer, nil, nil, content)
}

func (d *desktop) handResolverResults(r *handResolver) fyne.CanvasObject {
	title := widget.NewLabel("Results · strongest first")
	title.TextStyle = fyne.TextStyle{Bold: true}
	rows := container.New(layout.NewCustomPaddedVBoxLayout(8))
	if len(r.results) == 0 {
		rows.Add(mutedLabel("Resolve complete hands to see the ranking."))
	} else {
		for _, result := range r.results {
			name := strings.TrimSpace(r.players[result.player].name)
			if name == "" {
				name = fmt.Sprintf("Player %d", result.player+1)
			}
			row := widget.NewLabel(fmt.Sprintf("%s · %s\n%s", ordinalPlace(result.place), name, result.hand.Category.String()))
			row.Wrapping = fyne.TextWrapWord
			cards := container.NewHBox()
			for _, card := range result.hand.Cards {
				cards.Add(newResolverCardButton(poker.RankName(card.Rank), card.Suit, resolverResultCardSize, true, false, false, nil))
			}
			rows.Add(container.NewVBox(row, cards))
		}
	}
	return container.NewBorder(container.NewPadded(title), nil, nil, nil, container.NewVScroll(rows))
}

func resolverButtonCell(button *widget.Button, width float32) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(width, 40), button)
}

func resolverNameText(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) > 9 {
		return string(runes[:8]) + "…"
	}
	return string(runes)
}

func resolverNameColumnWidth() float32 {
	return fyne.MeasureText(strings.Repeat("M", 9), 16, fyne.TextStyle{}).Width + 8
}

func (d *desktop) resolverSlotButton(r *handResolver, slot resolverSlot, card *poker.Card) fyne.CanvasObject {
	rank, suit, hasSuit := "—", poker.Suit(0), false
	if card != nil {
		rank, suit, hasSuit = poker.RankName(card.Rank), card.Suit, true
	}
	return newResolverCardButton(rank, suit, resolverSlotSize, hasSuit, sameResolverSlot(r.active, slot), !r.slotAllowed(slot), func() {
		d.model.setResolverActive(slot)
		d.render(true)
	})
}

func (d *desktop) resolverDeck(r *handResolver) fyne.CanvasObject {
	ranks := make([]uint8, 0, 13)
	for rank := uint8(2); rank <= 14; rank++ {
		ranks = append(ranks, rank)
	}
	objects := make([]fyne.CanvasObject, 0, 14*5)
	objects = append(objects, widget.NewLabel(""))
	for _, rank := range ranks {
		objects = append(objects, widget.NewLabel(poker.RankName(rank)))
	}
	suits := []poker.Suit{poker.Hearts, poker.Clubs, poker.Spades, poker.Diamonds}
	for _, suit := range suits {
		label := canvas.NewText(suitLetter(suit), Spacedust.Foreground)
		label.Alignment, label.TextSize = fyne.TextAlignCenter, 18
		label.TextStyle = fyne.TextStyle{Bold: true}
		objects = append(objects, label)
		for _, rank := range ranks {
			card := poker.Card{Rank: rank, Suit: suit}
			button := newResolverCardButton(poker.RankName(rank), suit, resolverDeckSize, true, false, !r.slotAllowed(r.active) || r.cardUsed(card, resolverSlot{}), func() {
				d.model.selectResolverCard(card)
				d.render(true)
			})
			objects = append(objects, button)
		}
	}
	return container.New(&resolverDeckLayout{}, objects...)
}

type resolverColumnsLayout struct{}

func (resolverColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 {
		return fyne.NewSize(0, 0)
	}
	left, deck := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(left.Width+max(float32(828), deck.Width), max(left.Height, deck.Height))
}

func (resolverColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	leftMin := objects[0].MinSize().Width
	deckMin := float32(828)
	leftWidth := max(leftMin, min(size.Width*0.29, size.Width-deckMin))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(leftWidth, size.Height))
	objects[1].Move(fyne.NewPos(leftWidth, 0))
	objects[1].Resize(fyne.NewSize(size.Width-leftWidth, size.Height))
}

const resolverDeckResultsGap = 12

type resolverDeckResultsLayout struct{}

func (resolverDeckResultsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 {
		return fyne.NewSize(0, 0)
	}
	return fyne.NewSize(resolverDeckSize.Width*14+resolverDeckResultsGap+200, max(objects[0].MinSize().Height, objects[1].MinSize().Height))
}

func (resolverDeckResultsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	deckWidth := min(resolverDeckSize.Width*14, max(float32(0), size.Width-200-resolverDeckResultsGap))
	resultsX := deckWidth + resolverDeckResultsGap
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(deckWidth, size.Height))
	objects[1].Move(fyne.NewPos(resultsX, 0))
	objects[1].Resize(fyne.NewSize(size.Width-resultsX, size.Height))
}

type resolverDeckLayout struct{}

func (resolverDeckLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(resolverDeckSize.Width*14, 32+resolverDeckSize.Height*4)
}

func (resolverDeckLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	cellWidth := resolverDeckSize.Width
	cardHeight, headerHeight := resolverDeckSize.Height, float32(32)
	for i, object := range objects {
		row, column := i/14, i%14
		y, height := headerHeight+float32(row-1)*cardHeight, cardHeight
		if row == 0 {
			y, height = 0, headerHeight
		}
		object.Move(fyne.NewPos(float32(column)*cellWidth, y))
		object.Resize(fyne.NewSize(cellWidth, height))
	}
}

type resolverCardButton struct {
	widget.BaseWidget
	rank     string
	suit     poker.Suit
	size     fyne.Size
	hasSuit  bool
	active   bool
	disabled bool
	tapped   func()
}

func newResolverCardButton(rank string, suit poker.Suit, size fyne.Size, hasSuit, active, disabled bool, tapped func()) *resolverCardButton {
	button := &resolverCardButton{rank: rank, suit: suit, size: size, hasSuit: hasSuit, active: active, disabled: disabled, tapped: tapped}
	button.ExtendBaseWidget(button)
	return button
}

func (b *resolverCardButton) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(Spacedust.Raised)
	background.CornerRadius = 7
	background.StrokeColor, background.StrokeWidth = Spacedust.Line, 1
	rank := canvas.NewText(b.rank, Spacedust.Foreground)
	rank.Alignment, rank.TextSize = fyne.TextAlignCenter, 18
	rank.TextStyle = fyne.TextStyle{Bold: b.hasSuit}
	suit := canvas.NewText(suitName(b.suit), resolverSuitColor(b.suit))
	suit.Alignment, suit.TextSize = fyne.TextAlignCenter, 18
	suit.TextStyle = fyne.TextStyle{Symbol: true, Bold: true}
	if !b.hasSuit {
		suit.Hide()
	}
	renderer := &resolverCardButtonRenderer{button: b, background: background, rank: rank, suit: suit, objects: []fyne.CanvasObject{background, rank, suit}}
	renderer.Refresh()
	return renderer
}

func (b *resolverCardButton) Tapped(*fyne.PointEvent) {
	if !b.disabled && b.tapped != nil {
		b.tapped()
	}
}

func (b *resolverCardButton) MinSize() fyne.Size { return b.size }

var _ fyne.Tappable = (*resolverCardButton)(nil)

type resolverCardButtonRenderer struct {
	button     *resolverCardButton
	background *canvas.Rectangle
	rank       *canvas.Text
	suit       *canvas.Text
	objects    []fyne.CanvasObject
}

func (r *resolverCardButtonRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	if r.button.hasSuit {
		padding := max(float32(4), size.Width*0.07)
		r.rank.Alignment = fyne.TextAlignLeading
		r.rank.TextSize = max(float32(12), min(float32(24), size.Width*0.25))
		r.rank.Move(fyne.NewPos(padding, padding))
		r.rank.Resize(fyne.NewSize(size.Width*0.6, size.Height*0.24))
		r.suit.TextSize = max(float32(18), min(float32(42), size.Width*0.44))
		r.suit.Move(fyne.NewPos(0, size.Height*0.22))
		r.suit.Resize(fyne.NewSize(size.Width, size.Height*0.72))
	} else {
		r.rank.Alignment = fyne.TextAlignCenter
		r.rank.TextSize = 20
		r.rank.Resize(size)
	}
}

func (r *resolverCardButtonRenderer) MinSize() fyne.Size { return r.button.MinSize() }

func (r *resolverCardButtonRenderer) Refresh() {
	button := r.button
	r.background.FillColor, r.background.StrokeColor = Spacedust.Raised, Spacedust.Line
	r.rank.Text, r.rank.Color = button.rank, Spacedust.Foreground
	if button.hasSuit {
		r.background.FillColor = resolverCardFace
		r.rank.Color, r.suit.Color = resolverBlackSuit, resolverSuitColor(button.suit)
		r.suit.Text = suitName(button.suit)
		r.suit.Show()
		if button.disabled {
			r.background.FillColor = resolverDisabledCardFace
		}
	} else {
		r.suit.Hide()
		if button.disabled {
			r.rank.Color, r.background.FillColor = Spacedust.Muted, Spacedust.Panel
		}
	}
	if button.active && !button.disabled {
		r.background.StrokeColor = Spacedust.Accent
		r.background.StrokeWidth = 3
		if !button.hasSuit {
			r.background.FillColor, r.rank.Color = Spacedust.Accent, Spacedust.Background
		}
	} else {
		r.background.StrokeWidth = 1
	}
	r.background.Refresh()
	r.rank.Refresh()
	r.suit.Refresh()
}

func (r *resolverCardButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (*resolverCardButtonRenderer) Destroy()                       {}

var _ fyne.WidgetRenderer = (*resolverCardButtonRenderer)(nil)
