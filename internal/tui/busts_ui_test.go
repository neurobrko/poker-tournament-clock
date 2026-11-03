package tui

import (
	"testing"

	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestBuildBusteesPreservesOrderAndPartialTieGroups(t *testing.T) {
	got := buildBustees([]string{"a", "b", "c", "d", "e"}, map[string]bool{"a": true, "b": true, "d": true, "e": true})
	want := []tournament.Bustee{
		{PlayerID: "a", Position: 1, Tied: true},
		{PlayerID: "b", Position: 1, Tied: true},
		{PlayerID: "c", Position: 2},
		{PlayerID: "d", Position: 3, Tied: true},
		{PlayerID: "e", Position: 3, Tied: true},
	}
	if len(got) != len(want) {
		t.Fatalf("bustee groups = %+v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("bustee %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestBustSelectionCarriesBustersIntoSeparateBusteeOrder(t *testing.T) {
	model := NewModel()
	players := []tournament.Player{{ID: "a", Name: "A", Active: true}, {ID: "b", Name: "B", Active: true}, {ID: "c", Name: "C", Active: true}}
	model.players = players
	selection := &dialog{
		kind: dialogMultiPlayers, purpose: selectBuster,
		players: players, selected: map[string]bool{"a": true, "b": true},
		selectedOrder: []string{"a", "b"},
	}
	model.advanceBustSelection(selection)
	if model.modal == nil || model.modal.purpose != selectBustee {
		t.Fatalf("next bust selection = %+v", model.modal)
	}
	if len(model.modal.busters) != 2 || model.modal.busters[0] != "a" || model.modal.busters[1] != "b" || len(model.modal.selectedOrder) != 0 {
		t.Fatalf("buster/bustee selection state = %+v", model.modal)
	}
}
