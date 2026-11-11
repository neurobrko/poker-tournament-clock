package ui

import (
	"reflect"
	"testing"

	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
)

func TestBuildBusteesPreservesOrderAndPartialTieGroups(t *testing.T) {
	got := buildBustees([]string{"a", "b", "c", "d", "e"}, map[string]bool{"a": true, "b": true, "d": true, "e": true})
	want := []tournament.Bustee{
		{PlayerID: "a", Position: 3, Tied: true},
		{PlayerID: "b", Position: 3, Tied: true},
		{PlayerID: "c", Position: 2},
		{PlayerID: "d", Position: 1, Tied: true},
		{PlayerID: "e", Position: 1, Tied: true},
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

func TestBustPlacesAreHiddenWhileRebuysRemainOpen(t *testing.T) {
	model := NewModel()
	model.record.Status = store.StatusRunning
	model.record.Config.Registration.RebuysEnabled = true
	if bustPlacesVisible(model) {
		t.Fatal("bust places should be hidden while rebuys remain open")
	}
	model.record.RegistrationClosed = true
	if !bustPlacesVisible(model) {
		t.Fatal("bust places should be shown after registration closes")
	}
	model.record.RegistrationClosed = false
	model.record.Config.Registration.RebuysEnabled = false
	if !bustPlacesVisible(model) {
		t.Fatal("bust places should be shown when rebuys are disabled")
	}
}

func TestBusteeFinishingLabelsUseTournamentPlaceNumbers(t *testing.T) {
	labels := busteeFinishingLabels(5, []string{"worst", "better"}, nil)
	if labels["worst"] != "5th place" || labels["better"] != "4th place" {
		t.Fatalf("global bustee places = %+v", labels)
	}
	labels = busteeFinishingLabels(5, []string{"tied-a", "tied-b", "best"}, map[string]bool{"tied-a": true, "tied-b": true})
	if labels["tied-a"] != "4th place–5th place (tie)" || labels["tied-b"] != labels["tied-a"] || labels["best"] != "3rd place" {
		t.Fatalf("global places with tie = %+v", labels)
	}
}

func TestReorderBusteesMovesTiedPlayersAsOneFinishingGroup(t *testing.T) {
	order := []string{"a", "b", "c", "d"}
	tied := map[string]bool{"a": true, "b": true}
	order = reorderBustees(order, tied, "a", 1)
	if got, want := order, []string{"c", "a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("move tied group later = %v, want %v", got, want)
	}
	bustees := buildBustees(order, tied)
	if bustees[1].Position != 2 || bustees[2].Position != 2 || !bustees[1].Tied || !bustees[2].Tied {
		t.Fatalf("reordering split tie group: %+v", bustees)
	}
	order = reorderBustees(order, tied, "b", -1)
	if got, want := order, []string{"a", "b", "c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("move tied group earlier = %v, want %v", got, want)
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
