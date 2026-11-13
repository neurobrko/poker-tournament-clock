package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"git.mpx.sk/elvis/poker_clock/internal/poker"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestHandResolverEntryAndEscapeRestoreOrigin(t *testing.T) {
	for _, origin := range []screen{screenStartup, screenMain} {
		model := NewModel()
		model.screen = origin
		model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'h'}})
		if model.screen != screenResolver || model.resolver == nil || len(model.resolver.players) != 2 || model.resolver.community == nil || model.resolver.active.group != resolverCommunity {
			t.Fatalf("H from screen %d did not open resolver with two players", origin)
		}
		for i := 0; i < 5; i++ {
			model.onKey(keyEvent{Type: keyTab})
		}
		if model.resolver.active.group != resolverPlayer || model.resolver.active.player != 0 || model.resolver.active.index != 0 {
			t.Fatalf("Tab did not reach first hole slot: %+v", model.resolver.active)
		}
		model.setStatus("temporary resolver status", true)
		model.onKey(keyEvent{Type: keyEsc})
		if model.screen != origin || model.resolver != nil || model.statusMessage != "" {
			t.Fatalf("Escape returned to screen %d with resolver %v", model.screen, model.resolver)
		}
	}
}

func TestResolverKeyboardSelectionUniquenessCorrectionAndUndo(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	selectCard(model, 'A', 'H')
	selectCard(model, 'A', 'H') // duplicate must not fill the next slot
	if model.resolver.community[0] == nil || model.resolver.community[1] != nil {
		t.Fatalf("duplicate card selection changed board: %+v", model.resolver.community)
	}

	model.setResolverActive(resolverSlot{group: resolverCommunity, index: 0, valid: true})
	selectCard(model, 'A', 'C')
	if got := model.resolver.community[0]; got == nil || *got != (poker.Card{Rank: 14, Suit: poker.Clubs}) {
		t.Fatalf("correction did not replace selected slot: %v", got)
	}
	model.setResolverActive(resolverSlot{group: resolverCommunity, index: 0, valid: true})
	model.onKey(keyEvent{Type: keyBackspace})
	if model.resolver.community[0] != nil {
		t.Fatal("Backspace did not return the last selected card to the deck")
	}
	if model.resolver.cardUsed(poker.Card{Rank: 14, Suit: poker.Hearts}, resolverSlot{}) {
		t.Fatal("replaced card remained unavailable")
	}
}

func TestResolverTenAliasesAndFiveCardBoardRequired(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	model.resolver.community = nil
	model.resolveHands()
	if !model.statusIsError {
		t.Fatal("resolving without community board should fail")
	}
	model.resolver.community = &[5]*poker.Card{}
	model.resolver.active = resolverSlot{group: resolverCommunity, index: 0, valid: true}
	for i, value := range []rune{'0', '1', '2', '3', '4'} {
		suit := 'S'
		if i == 1 {
			suit = 'H'
		}
		model.onKey(keyEvent{Type: keyRunes, Runes: []rune{value}})
		model.onKey(keyEvent{Type: keyRunes, Runes: []rune{suit}})
	}
	want := []uint8{10, 10, 2, 3, 4}
	for i, rank := range want {
		if model.resolver.community[i] == nil || model.resolver.community[i].Rank != rank {
			t.Fatalf("community card %d = %v, want rank %d", i, model.resolver.community[i], rank)
		}
	}
	model.resolveHands()
	if model.statusMessage != "Each player needs two hole cards:" {
		t.Fatalf("resolve status = %q, want incomplete-player warning", model.statusMessage)
	}
}

func TestResolverShowsTiesAndAllowsPlayerManagement(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	for _, card := range []poker.Card{
		{Rank: 14, Suit: poker.Spades}, {Rank: 13, Suit: poker.Spades}, {Rank: 12, Suit: poker.Spades}, {Rank: 11, Suit: poker.Spades}, {Rank: 10, Suit: poker.Spades},
	} {
		model.selectResolverCard(card)
	}
	for _, card := range []poker.Card{
		{Rank: 2, Suit: poker.Hearts}, {Rank: 3, Suit: poker.Hearts},
		{Rank: 4, Suit: poker.Hearts}, {Rank: 5, Suit: poker.Hearts},
	} {
		model.selectResolverCard(card)
	}
	model.resolveHands()
	if len(model.resolver.results) != 2 || model.resolver.results[0].place != 1 || model.resolver.results[1].place != 1 {
		t.Fatalf("board-only straight flush did not tie: %+v", model.resolver.results)
	}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'+'}})
	if len(model.resolver.players) != 3 || model.resolver.results != nil {
		t.Fatalf("adding player did not clear stale results: %+v", model.resolver)
	}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'-'}})
	if model.modal == nil || model.modal.kind != dialogMultiPlayers || len(model.modal.players) != 3 {
		t.Fatalf("minus did not open multi-player selector: %+v", model.modal)
	}
	model.onKey(keyEvent{Type: keySpace})
	model.onKey(keyEvent{Type: keyEnter})
	if len(model.resolver.players) != 2 || model.resolver.players[0].name != "Player 2" {
		t.Fatalf("selected player removal failed: %+v", model.resolver.players)
	}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'-'}})
	if model.modal != nil || len(model.resolver.players) != 2 {
		t.Fatalf("player removal went below minimum: %d", len(model.resolver.players))
	}
}

func TestResolverEditButtonSelectsPlayerBeforeRenaming(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	model.addResolverPlayer()
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'e'}})
	if model.modal == nil || model.modal.kind != dialogMenu || len(model.modal.choices) != 3 {
		t.Fatalf("Edit name did not open player picker: %+v", model.modal)
	}
	model.onKey(keyEvent{Type: keyDown})
	model.onKey(keyEvent{Type: keyEnter})
	if model.modal == nil || model.modal.textPurpose != textResolverPlayerName || model.modal.resolverPlayer != 1 {
		t.Fatalf("player picker chose wrong edit target: %+v", model.modal)
	}
	model.modal.input = "Ada"
	model.submitText(model.modal)
	if model.resolver.players[1].name != "Ada" {
		t.Fatalf("renamed player = %q, want Ada", model.resolver.players[1].name)
	}
}

func TestResolverResetClearsBoardAndHoleCards(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	model.resolver.community[0] = &poker.Card{Rank: 14, Suit: poker.Hearts}
	model.resolver.players[0].hole[0] = &poker.Card{Rank: 13, Suit: poker.Clubs}
	model.resolver.results = []resolverResult{{player: 0}}
	model.resolver.active = resolverSlot{group: resolverPlayer, player: 0, index: 0, valid: true}
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{'r'}})
	if model.resolver.community[0] != nil || model.resolver.players[0].hole[0] != nil || len(model.resolver.results) != 0 || model.resolver.active.group != resolverCommunity || model.resolver.active.index != 0 {
		t.Fatalf("reset left card state behind: %+v", model.resolver)
	}
}

func TestResolverMinusRemovesSeveralSelectedPlayers(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	model.addResolverPlayer()
	model.addResolverPlayer()
	model.requestResolverPlayerRemoval()
	model.onKey(keyEvent{Type: keySpace}) // Player 1
	model.onKey(keyEvent{Type: keyDown})
	model.onKey(keyEvent{Type: keyDown})
	model.onKey(keyEvent{Type: keySpace}) // Player 3
	model.onKey(keyEvent{Type: keyDown})
	model.onKey(keyEvent{Type: keySpace}) // Cannot select third removal; keep two.
	if len(model.modal.selectedOrder) != 2 {
		t.Fatalf("selected removals = %v, want two", model.modal.selectedOrder)
	}
	model.onKey(keyEvent{Type: keyEnter})
	if len(model.resolver.players) != 2 || model.resolver.players[0].name != "Player 2" || model.resolver.players[1].name != "Player 4" {
		t.Fatalf("wrong players remain after multi-remove: %+v", model.resolver.players)
	}
}

func TestHandResolverLoadsTournamentActivePlayers(t *testing.T) {
	model := NewModel()
	model.players = []tournament.Player{
		{ID: "a", Name: "Ada", Active: true},
		{ID: "b", Name: "Inactive", Active: false},
		{ID: "c", Name: "Sitting out", Active: true, SittingOut: true},
		{ID: "d", Name: "Removed", Active: true, Removed: true},
		{ID: "e", Name: "Bea", Active: true},
	}
	model.openHandResolver(screenMain)
	if len(model.resolver.players) != 3 || model.resolver.players[0].name != "Ada" || model.resolver.players[1].name != "Sitting out" || model.resolver.players[2].name != "Bea" {
		t.Fatalf("resolver roster = %+v", model.resolver.players)
	}
}

func TestResolverSuitColorsAndLetters(t *testing.T) {
	if resolverSuitColor(poker.Hearts) != resolverRedSuit || resolverSuitColor(poker.Diamonds) != resolverRedSuit {
		t.Fatal("hearts and diamonds must use red ink")
	}
	if resolverSuitColor(poker.Spades) != resolverBlackSuit || resolverSuitColor(poker.Clubs) != resolverBlackSuit {
		t.Fatal("spades and clubs must use black ink")
	}
	for suit, want := range map[poker.Suit]string{poker.Hearts: "H", poker.Clubs: "C", poker.Spades: "S", poker.Diamonds: "D"} {
		if got := suitLetter(suit); got != want {
			t.Errorf("suit legend = %q, want %q", got, want)
		}
	}
	card := newResolverCardButton("A", poker.Hearts, resolverSlotSize, true, false, false, nil)
	renderer := card.CreateRenderer().(*resolverCardButtonRenderer)
	if renderer.rank.Color != resolverBlackSuit || renderer.suit.Color != resolverRedSuit {
		t.Fatalf("heart card colors rank=%v suit=%v", renderer.rank.Color, renderer.suit.Color)
	}
	if card.MinSize().Width >= card.MinSize().Height || renderer.background.CornerRadius == 0 || resolverDeckSize != resolverSlotSize {
		t.Fatal("deck, board, and hole cards must share a rounded portrait size")
	}
}

func TestResolverPlayerKeysWorkThroughFyneKeyEvents(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("resolver keys")
	model := NewModel()
	model.openHandResolver(screenStartup)
	d := newTestDesktop(app, window, model)
	window.Resize(fyne.NewSize(1280, 800))
	d.render(true)
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyEqual})
	d.dispatchRune('+') // Fyne may report both key and rune; do not double-add.
	if len(model.resolver.players) != 3 {
		t.Fatalf("plus key produced %d players, want 3", len(model.resolver.players))
	}
	d.dispatchKeyEvent(&fyne.KeyEvent{Name: fyne.KeyMinus})
	if model.modal == nil || model.modal.kind != dialogMultiPlayers {
		t.Fatalf("minus key did not open player selector: %+v", model.modal)
	}
}

func TestHandResolverDesktopViewBuilds(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("resolver")
	model := NewModel()
	model.openHandResolver(screenStartup)
	d := newTestDesktop(app, window, model)
	window.Resize(fyne.NewSize(1280, 800))
	d.render(true)
	if window.Canvas().Content() == nil {
		t.Fatal("resolver view has no content")
	}
	window.Show()
}

func TestResolverResultsColumnHasGapAfterDeck(t *testing.T) {
	objects := []fyne.CanvasObject{widget.NewLabel("deck"), widget.NewLabel("results")}
	layout := resolverDeckResultsLayout{}
	layout.Layout(objects, layout.MinSize(objects))
	if got := objects[1].Position().X - (objects[0].Position().X + objects[0].Size().Width); got != resolverDeckResultsGap {
		t.Fatalf("deck/results gap = %v, want %v", got, resolverDeckResultsGap)
	}
}

func TestResolverDeckRendersUniqueSelectable52CardGrid(t *testing.T) {
	model := NewModel()
	model.openHandResolver(screenStartup)
	model.selectResolverCard(poker.Card{Rank: 14, Suit: poker.Spades})
	deck := (&desktop{model: model}).resolverDeck(model.resolver).(*fyne.Container)
	if len(deck.Objects) != 70 { // header row plus four suit rows, each 14 cells
		t.Fatalf("deck cells = %d, want 70", len(deck.Objects))
	}
	deckLayout := resolverDeckLayout{}
	deckLayout.Layout(deck.Objects, fyne.NewSize(14*resolverDeckSize.Width, 800))
	if legend, ok := deck.Objects[14].(*canvas.Text); !ok || legend.Color != Spacedust.Foreground || deck.Objects[15].Position().Y != deck.Objects[0].Size().Height || deck.Objects[69].Position().Y+deck.Objects[69].Size().Height != 32+4*resolverDeckSize.Height {
		t.Fatal("white suit legend, zero header gap, and fixed deck card size should be preserved")
	}
	count, foundDisabledAce := 0, false
	for _, object := range deck.Objects {
		if card, ok := object.(*resolverCardButton); ok {
			count++
			if card.rank == "A" && card.suit == poker.Spades && card.hasSuit {
				foundDisabledAce = card.disabled
			}
		}
	}
	if count != 52 || !foundDisabledAce {
		t.Fatalf("deck cards = %d, selected ace disabled = %t", count, foundDisabledAce)
	}
}

func selectCard(model *Model, rank, suit rune) {
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{rank}})
	model.onKey(keyEvent{Type: keyRunes, Runes: []rune{suit}})
}
