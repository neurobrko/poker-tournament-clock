package tui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"git.mpx.sk/elvis/poker_clock/internal/domain"
	"git.mpx.sk/elvis/poker_clock/internal/preferences"
	"git.mpx.sk/elvis/poker_clock/internal/sleepinhibit"
	"git.mpx.sk/elvis/poker_clock/internal/sound"
	"git.mpx.sk/elvis/poker_clock/internal/store"
	"git.mpx.sk/elvis/poker_clock/internal/tournament"
	tea "github.com/charmbracelet/bubbletea"
)

//go:embed rules.md
var embeddedDocs embed.FS

type clockTickMsg time.Time

type screen int

const (
	screenStartup screen = iota
	screenMain
	screenRules
)

type Model struct {
	screen screen

	width, height int
	tooSmall      bool
	startupCursor int

	browser         *fileBrowser
	browserCancel   screen
	pendingName     string
	pendingTemplate config.Template

	database    *store.Store
	service     *tournament.Service
	record      store.TournamentRecord
	summary     tournament.Summary
	players     []tournament.Player
	placePrizes []domain.PlacePrize

	pendingEndExplanation  string
	pendingEndChoices      map[int][]string
	pendingPrizeGroups     []domain.PrizeGroup
	pendingPrizeGroupIndex int
	pendingRemainderPlace  int
	pendingRemainderCount  int

	modal          *dialog
	suspendedModal *dialog
	rulesText      string
	rulesOffset    int

	statusMessage string
	statusIsError bool
	notice        string

	lastTick time.Time
	lastWall time.Time

	prefs     preferences.Preferences
	inhibitor *sleepinhibit.Inhibitor
}

func NewModel() *Model {
	model := &Model{screen: screenStartup, width: 120, height: 36, prefs: preferences.Default()}
	if prefs, err := preferences.Load(); err == nil {
		model.prefs = prefs
	} else {
		model.setStatus(err, true)
	}
	if data, err := embeddedDocs.ReadFile("rules.md"); err == nil {
		model.rulesText = string(data)
	} else {
		model.rulesText = "Tournament rules reference unavailable."
	}
	return model
}

func (m *Model) Init() tea.Cmd { return nextClockTick() }

func nextClockTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return clockTickMsg(now) })
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		wasSmall := m.tooSmall
		m.width, m.height = msg.Width, msg.Height
		m.tooSmall = msg.Width < 120 || msg.Height < 36
		if wasSmall && !m.tooSmall && m.modal == nil && m.suspendedModal != nil {
			m.modal, m.suspendedModal = m.suspendedModal, nil
		}
		return m, nil
	case clockTickMsg:
		m.onClockTick(time.Time(msg))
		return m, nextClockTick()
	case tea.KeyMsg:
		return m.onKey(msg)
	default:
		return m, nil
	}
}

func (m *Model) onClockTick(tick time.Time) {
	wall := time.Now()
	if m.lastTick.IsZero() {
		m.lastTick = tick
		m.lastWall = stripMonotonic(wall)
		return
	}
	delta := tick.Sub(m.lastTick)
	wallNow := stripMonotonic(wall)
	wallDelta := wallNow.Sub(m.lastWall)
	m.processClockDelta(delta, wallDelta, wall)
	m.lastTick, m.lastWall = tick, wallNow
}

func (m *Model) processClockDelta(delta, wallDelta time.Duration, wall time.Time) {
	if m.service == nil {
		return
	}
	if wallDelta > delta+2*time.Second {
		if m.record.Status == store.StatusRunning {
			if err := m.service.PauseAfterSleep(context.Background(), wall); err != nil {
				m.setStatus(err, true)
			} else {
				m.notice = "Computer sleep detected — clock paused at last checkpoint."
				m.setStatus(errors.New(m.notice), true)
			}
		} else if m.record.Status == store.StatusPaused {
			m.notice = "Computer sleep detected; tournament clock was already paused."
		}
		m.refresh()
		return
	}
	if m.record.Status == store.StatusRunning && delta > 0 {
		transitions, err := m.service.AdvanceClock(context.Background(), delta, wall)
		if err != nil {
			m.setStatus(err, true)
		} else if len(transitions) > 0 {
			m.notice = transitionNotice(transitions, m.record.Config.Schedule)
			if soundErr := sound.Play(m.prefs.SoundFile, m.prefs.SoundEnabled); soundErr != nil {
				m.setStatus(soundErr, true)
			}
		}
	}
	m.refresh()
}

func (m *Model) syncClock() {
	if m.service == nil {
		return
	}
	now := time.Now()
	if m.lastTick.IsZero() {
		m.lastTick = now
		m.lastWall = stripMonotonic(now)
		return
	}
	delta := now.Sub(m.lastTick)
	wallNow := stripMonotonic(now)
	wallDelta := wallNow.Sub(m.lastWall)
	m.processClockDelta(delta, wallDelta, now)
	m.lastTick = now
	m.lastWall = wallNow
}

func stripMonotonic(value time.Time) time.Time {
	return time.Unix(0, value.UnixNano()).In(value.Location())
}

func (m *Model) actionContext() tournament.ActionContext {
	m.syncClock()
	now := time.Now()
	action := tournament.ActionContext{WallTime: now}
	if m.record.StartedAt != nil {
		checkpoint := m.record.Checkpoint
		if checkpoint.RealElapsed < now.Sub(*m.record.StartedAt) {
			checkpoint.RealElapsed = now.Sub(*m.record.StartedAt)
		}
		action.Checkpoint = &checkpoint
	}
	return action
}

func (m *Model) refresh() {
	if m.database == nil || m.service == nil {
		return
	}
	record, err := m.database.Tournament(context.Background())
	if err != nil {
		m.setStatus(err, true)
		return
	}
	m.record = record
	if summary, err := m.service.CurrentSummary(context.Background()); err == nil {
		m.summary = summary
	} else {
		m.setStatus(err, true)
	}
	if players, err := m.service.Players(context.Background(), true); err == nil {
		m.players = players
	} else {
		m.setStatus(err, true)
	}
	if placePrizes, err := domain.CalculatePlacePrizes(int64(m.summary.PrizePoolCents), int(m.summary.Entrants), m.record.Config.PrizeTiers); err == nil {
		m.placePrizes = placePrizes
	} else {
		m.placePrizes = nil
		m.setStatus(err, true)
	}
}

func (m *Model) openTournament(database *store.Store) {
	m.database = database
	m.service = tournament.NewService(database)
	m.screen = screenMain
	m.modal = nil
	m.browser = nil
	m.lastTick = time.Now()
	m.lastWall = stripMonotonic(m.lastTick)
	m.refresh()
	m.inhibitor = sleepinhibit.New()
	if err := m.inhibitor.Start(); err != nil {
		m.setStatus("Warning: "+err.Error(), false)
	} else {
		m.setStatus("Sleep/display inactivity inhibition is active while tournament is open.", false)
	}
}

func (m *Model) closeTournament() error {
	if m.database != nil && m.service != nil {
		m.syncClock()
		m.refresh()
		if m.record.Status == store.StatusRunning {
			checkpoint := m.record.Checkpoint
			if err := m.service.PauseTournament(context.Background(), tournament.ActionContext{WallTime: time.Now(), Checkpoint: &checkpoint}); err != nil {
				return fmt.Errorf("pause and save before exit: %w", err)
			}
		}
	}
	var firstErr error
	if m.inhibitor != nil {
		if err := m.inhibitor.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.inhibitor = nil
	}
	if m.database != nil {
		if err := m.database.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.database, m.service = nil, nil
	}
	return firstErr
}

func (m *Model) onKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyCtrlC {
		m.askQuit()
		return m, nil
	}
	if m.tooSmall {
		return m.handleSmallTerminalKey(key)
	}
	if m.modal != nil {
		if key.Type == tea.KeyRunes && strings.EqualFold(key.String(), "q") && m.modal.kind != dialogText {
			m.suspendedModal = m.modal
			m.modal = nil
			m.askQuit()
			return m, nil
		}
		return m.handleDialogKey(key)
	}
	if m.browser != nil {
		if key.Type == tea.KeyRunes && strings.EqualFold(key.String(), "q") {
			m.askQuit()
			return m, nil
		}
		return m.handleBrowserKey(key)
	}
	if m.screen == screenRules {
		return m.handleRulesKey(key)
	}
	if m.screen == screenStartup {
		return m.handleStartupKey(key)
	}
	return m.handleMainKey(key)
}

func (m *Model) handleSmallTerminalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal != nil && m.modal.kind == dialogConfirm {
		if key.Type == tea.KeyEnter || key.Type == tea.KeyEsc || key.Type == tea.KeyRunes && (strings.EqualFold(key.String(), "y") || strings.EqualFold(key.String(), "n")) {
			return m.handleConfirmDialog(key, m.modal)
		}
	}
	if key.Type != tea.KeyRunes && key.Type != tea.KeySpace {
		return m, nil
	}
	if key.Type == tea.KeyRunes && strings.EqualFold(key.String(), "q") {
		m.askQuit()
		return m, nil
	}
	if key.Type == tea.KeySpace {
		if m.modal != nil {
			m.suspendedModal = m.modal
			m.modal = nil
		}
		m.toggleClock()
		return m, nil
	}
	return m, nil
}

func (m *Model) handleStartupKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		m.startupCursor = (m.startupCursor + 2) % 3
	case tea.KeyDown:
		m.startupCursor = (m.startupCursor + 1) % 3
	case tea.KeyEnter:
		switch m.startupCursor {
		case 0:
			m.modal = &dialog{kind: dialogText, title: "New tournament", prompt: "Tournament name", textPurpose: textTournamentName}
		case 1:
			m.startBrowser(browseTournamentDB, screenStartup)
		case 2:
			m.askQuit()
		}
	case tea.KeyEsc:
		m.askQuit()
	case tea.KeyRunes:
		switch strings.ToLower(key.String()) {
		case "n":
			m.startupCursor = 0
			m.modal = &dialog{kind: dialogText, title: "New tournament", prompt: "Tournament name", textPurpose: textTournamentName}
		case "l":
			m.startupCursor = 1
			m.startBrowser(browseTournamentDB, screenStartup)
		case "q":
			m.askQuit()
		}
	}
	return m, nil
}

func (m *Model) handleMainKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeySpace {
		m.toggleClock()
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		return m, nil
	}
	if key.Type != tea.KeyRunes {
		return m, nil
	}
	switch key.String() {
	case "q", "Q":
		m.askQuit()
	case "b", "B":
		m.openPurchase(tournament.PurchaseInitialBuyIn)
	case "r", "R":
		m.openPurchase(tournament.PurchaseRebuy)
	case "a", "A":
		m.openPurchase(tournament.PurchaseAddOn)
	case "p", "P":
		m.modal = &dialog{kind: dialogMenu, purpose: menuPlayers, title: "Players", choices: []string{"Add player", "Rename player", "Remove player", "Sit out / return", "Void purchase"}}
	case "x", "X":
		m.modal = &dialog{kind: dialogMenu, purpose: menuBusts, title: "Bust management", choices: []string{"Record bust", "Void a bust event", "Correct a bust participant"}}
	case "o", "O":
		m.openPlayerSelect(selectSitOut, activePlayers(m.players))
	case "l", "L":
		m.openLevelSelect()
	case "s", "S":
		m.openSettings()
	case "e", "E":
		m.beginEnd()
	case "h", "H":
		m.showMessage("Hand resolver", "Hand resolver is reserved for Stage 2.")
	case "?":
		m.screen = screenRules
		m.rulesOffset = 0
	case "n", "N":
		m.toggleSound()
	}
	return m, nil
}

func (m *Model) handleRulesKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeySpace:
		m.toggleClock()
	case tea.KeyEsc:
		m.screen = screenMain
	case tea.KeyUp:
		if m.rulesOffset > 0 {
			m.rulesOffset--
		}
	case tea.KeyDown:
		m.rulesOffset++
	case tea.KeyPgUp:
		m.rulesOffset -= 10
		if m.rulesOffset < 0 {
			m.rulesOffset = 0
		}
	case tea.KeyPgDown:
		m.rulesOffset += 10
	case tea.KeyRunes:
		if key.String() == "?" {
			m.screen = screenMain
		} else if strings.EqualFold(key.String(), "q") {
			m.askQuit()
		}
	}
	return m, nil
}

func (m *Model) handleBrowserKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.browser = nil
		m.screen = m.browserCancel
	case tea.KeyUp:
		m.browser.move(-1)
	case tea.KeyDown:
		m.browser.move(1)
	case tea.KeyBackspace:
		m.browser.directory = filepath.Dir(m.browser.directory)
		if err := m.browser.reload(); err != nil {
			m.setStatus(err, true)
		}
	case tea.KeyEnter:
		path, done, err := m.browser.enter()
		if err != nil {
			m.setStatus(err, true)
			return m, nil
		}
		if done {
			m.browserSelected(path)
		}
	case tea.KeyRunes:
		if strings.EqualFold(key.String(), "n") && (m.browser.purpose == browseSaveDatabase || m.browser.purpose == browseSaveTemplate) {
			m.modal = &dialog{kind: dialogText, title: "New file", prompt: "Filename (Enter for " + m.defaultFilename() + ")", textPurpose: textBrowserFilename}
		}
	}
	return m, nil
}

func (m *Model) browserSelected(path string) {
	purpose := m.browser.purpose
	m.browser = nil
	switch purpose {
	case browseTournamentDB:
		database, err := store.Open(context.Background(), path)
		if err != nil {
			m.showMessage("Load failed", err.Error())
			return
		}
		m.openTournament(database)
	case browseTemplate:
		if m.screen == screenMain {
			m.saveTemplateImport(path)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			m.showMessage("Template read failed", err.Error())
			return
		}
		template, err := config.ParseTemplate(data)
		if err != nil {
			m.showMessage("Invalid template", err.Error())
			return
		}
		m.pendingTemplate = template
		m.startBrowser(browseSaveDatabase, screenStartup)
	case browseSound:
		m.prefs.SoundFile = path
		if err := preferences.Save(m.prefs); err != nil {
			m.setStatus(err, true)
		} else {
			m.setStatus(fmt.Errorf("custom sound saved"), false)
		}
	case browseSaveDatabase, browseSaveTemplate:
		m.saveBrowserDestination(path, purpose)
	}
}

func (m *Model) saveBrowserDestination(path string, purpose browserPurpose) {
	switch purpose {
	case browseSaveDatabase:
		database, err := store.Create(context.Background(), path, m.pendingName, m.pendingTemplate)
		if err != nil {
			m.showMessage("Tournament creation failed", err.Error())
			return
		}
		m.openTournament(database)
	case browseSaveTemplate:
		data, err := config.MarshalTemplate(m.record.Config)
		if err != nil {
			m.showMessage("Export failed", err.Error())
			return
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			m.showMessage("Export failed", err.Error())
			return
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			m.showMessage("Export failed", err.Error())
			return
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			m.showMessage("Export failed", err.Error())
			return
		}
		if err := file.Close(); err != nil {
			m.showMessage("Export failed", err.Error())
			return
		}
		m.setStatus("Tournament template exported.", false)
	}
}

func (m *Model) startBrowser(purpose browserPurpose, cancelTo screen) {
	browser, err := newFileBrowser(purpose, "")
	if err != nil {
		m.showMessage("File browser failed", err.Error())
		return
	}
	m.browser = browser
	m.browserCancel = cancelTo
	m.modal = nil
}

func (m *Model) completeNewTemplateChoice(useDefault bool) {
	if useDefault {
		template, err := config.DefaultTemplate()
		if err != nil {
			m.showMessage("Default template failed", err.Error())
			return
		}
		m.pendingTemplate = template
		m.startBrowser(browseSaveDatabase, screenStartup)
		return
	}
	m.startBrowser(browseTemplate, screenStartup)
}

func (m *Model) defaultFilename() string {
	if m.browser == nil {
		return ""
	}
	base := strings.TrimSpace(m.pendingName)
	if base == "" && m.record.Name != "" {
		base = m.record.Name
	}
	base = strings.ToLower(strings.ReplaceAll(base, " ", "-"))
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, base)
	switch m.browser.purpose {
	case browseSaveTemplate:
		return base + ".yaml"
	case browseSaveDatabase:
		return base + ".sqlite"
	default:
		return ""
	}
}

func (m *Model) openPurchase(kind tournament.PurchaseKind) {
	players := make([]tournament.Player, 0)
	for _, player := range m.players {
		if player.Removed {
			continue
		}
		switch kind {
		case tournament.PurchaseInitialBuyIn:
			if !player.HasInitialBuyIn {
				players = append(players, player)
			}
		case tournament.PurchaseRebuy:
			if player.HasInitialBuyIn {
				players = append(players, player)
			}
		case tournament.PurchaseAddOn:
			if player.HasInitialBuyIn && player.Active {
				players = append(players, player)
			}
		}
	}
	if len(players) == 0 {
		m.showMessage("Purchase unavailable", "No eligible players are registered.")
		return
	}
	m.openPlayerSelect(selectPurchase, players)
	m.modal.purchaseKind = kind
}

func (m *Model) openPlayerSelect(purpose dialogPurpose, players []tournament.Player) {
	m.modal = &dialog{kind: dialogSelectPlayer, purpose: purpose, title: "Select player", players: players}
}

func (m *Model) openLevelSelect() {
	if m.service == nil {
		return
	}
	choices := make([]string, len(m.record.Config.Schedule))
	level, breakNo := 0, 0
	for index, segment := range m.record.Config.Schedule {
		if segment.Kind == config.SegmentLevel {
			level++
			choices[index] = fmt.Sprintf("Level %d — %d/%d", level, segment.SmallBlind, segment.BigBlind)
		} else {
			breakNo++
			label := segment.Label
			if label == "" {
				label = fmt.Sprintf("Break %d", breakNo)
			}
			choices[index] = label
		}
	}
	m.modal = &dialog{kind: dialogSelectSegment, title: "Select level or break", choices: choices}
}

func (m *Model) openSettings() {
	choices := []string{"Export reusable template", "Import/edit YAML template", "Rename tournament", "Choose sound file", "Clear custom sound", "Toggle sound"}
	if m.record.Status == store.StatusEnded {
		choices = append(choices, "Reopen ended tournament")
	}
	m.modal = &dialog{kind: dialogMenu, purpose: menuSettings, title: "Settings", choices: choices}
}

func (m *Model) beginEnd() {
	if m.record.Status == store.StatusEnded {
		m.showMessage("Tournament ended", "Reopen it from Settings before making further changes.")
		return
	}
	if m.service == nil || m.record.StartedAt == nil {
		m.showMessage("End tournament", "Tournament has not started.")
		return
	}
	m.syncClock()
	if m.summary.ActivePlayers == 1 || m.summary.Entrants == 1 {
		m.modal = &dialog{kind: dialogConfirm, title: "Finalize tournament?", message: "Only one active player remains. Ending closes registration/rebuys and finalizes the result.", confirm: confirmCompletedEnd}
		return
	}
	m.modal = &dialog{kind: dialogText, title: "End incomplete tournament", prompt: "Explanation (required, max 200 chars)", textPurpose: textIncompleteEnd}
}

func (m *Model) toggleClock() {
	if m.service == nil {
		return
	}
	m.syncClock()
	switch m.record.Status {
	case store.StatusNotStarted:
		m.modal = &dialog{kind: dialogConfirm, title: "Start tournament?", message: "Starting locks tournament settings. Start clock?", confirm: confirmStart}
	case store.StatusRunning:
		action := m.actionContextWithoutSync()
		if err := m.service.PauseTournament(context.Background(), action); err != nil {
			m.showMessage("Pause failed", err.Error())
		} else {
			m.refresh()
			m.setStatus("Clock paused.", false)
		}
	case store.StatusPaused:
		action := m.actionContextWithoutSync()
		transitions, err := m.service.ResumeTournament(context.Background(), action)
		if err != nil {
			m.showMessage("Resume failed", err.Error())
		} else {
			m.refresh()
			if len(transitions) > 0 {
				m.notice = transitionNotice(transitions, m.record.Config.Schedule)
			}
			m.setStatus("Clock resumed.", false)
		}
	case store.StatusEnded:
		m.showMessage("Tournament ended", "Reopen it from Settings to continue.")
	}
}

func (m *Model) actionContextWithoutSync() tournament.ActionContext {
	action := tournament.ActionContext{WallTime: time.Now()}
	if m.record.StartedAt != nil {
		checkpoint := m.record.Checkpoint
		if real := time.Since(*m.record.StartedAt); real > checkpoint.RealElapsed {
			checkpoint.RealElapsed = real
		}
		action.Checkpoint = &checkpoint
	}
	return action
}

func (m *Model) askQuit() {
	if m.modal != nil {
		m.suspendedModal = m.modal
	}
	m.modal = &dialog{kind: dialogConfirm, title: "Exit Poker Clock?", message: "The clock will be paused and saved. Tournament will not be ended.", confirm: confirmQuit}
}

func (m *Model) toggleSound() {
	m.prefs.SoundEnabled = !m.prefs.SoundEnabled
	if err := preferences.Save(m.prefs); err != nil {
		m.showMessage("Sound preference failed", err.Error())
		return
	}
	if m.prefs.SoundEnabled {
		m.setStatus("Sound enabled.", false)
	} else {
		m.setStatus("Sound disabled.", false)
	}
}

func (m *Model) finishQuitCommand() tea.Cmd {
	if err := m.closeTournament(); err != nil {
		m.showMessage("Exit warning", err.Error())
		return nil
	}
	return tea.Quit
}

func (m *Model) showMessage(title, message string) {
	m.modal = &dialog{kind: dialogMessage, title: title, message: message}
}

func (m *Model) setStatus(value any, isError bool) {
	if value == nil {
		m.statusMessage = ""
		m.statusIsError = false
		return
	}
	switch message := value.(type) {
	case error:
		m.statusMessage = message.Error()
	case string:
		m.statusMessage = message
	default:
		m.statusMessage = fmt.Sprint(message)
	}
	m.statusIsError = isError
}

func activePlayers(players []tournament.Player) []tournament.Player {
	var result []tournament.Player
	for _, player := range players {
		if player.Active && !player.Removed {
			result = append(result, player)
		}
	}
	return result
}
