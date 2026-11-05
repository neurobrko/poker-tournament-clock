package ui

import (
	"context"
	"embed"
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
)

//go:embed rules.md
var embeddedDocs embed.FS

type screen int

const (
	screenStartup screen = iota
	screenMain
	screenRules
)

type Model struct {
	screen screen

	startupCursor int

	browser         *fileBrowser
	browserCancel   screen
	pendingName     string
	pendingTemplate config.Template

	database             *store.Store
	service              *tournament.Service
	record               store.TournamentRecord
	summary              tournament.Summary
	players              []tournament.Player
	placePrizes          []domain.PlacePrize
	finalResultAvailable bool

	pendingEndExplanation  string
	pendingEndChoices      map[int][]string
	pendingPrizeGroups     []domain.PrizeGroup
	pendingPrizeGroupIndex int
	pendingRemainderPlace  int
	pendingRemainderCount  int

	modal          *dialog
	suspendedModal *dialog
	rulesText      string

	statusMessage string
	statusIsError bool
	notice        string

	lastTick               time.Time
	lastWall               time.Time
	clockPersistenceFailed bool

	prefs      preferences.Preferences
	inhibitor  *sleepinhibit.Inhibitor
	asyncError func(error)
}

func NewModel() *Model {
	model := &Model{screen: screenStartup, prefs: preferences.Default()}
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

func (m *Model) onClockTick() { m.syncClock() }

// processClockDelta returns false when persistence fails so the next update retries the full interval.
func (m *Model) processClockDelta(delta, wallDelta time.Duration, wall time.Time) bool {
	if m.service == nil {
		return true
	}
	if wallDelta > delta+2*time.Second {
		if m.record.Status == store.StatusRunning {
			if err := m.service.PauseAfterSleep(context.Background(), wall); err != nil {
				m.clockPersistenceFailed = true
				m.setStatus(err, true)
				m.refreshClock()
				return false
			}
			m.notice = "Computer sleep detected — clock paused at last checkpoint."
			m.setStatus(m.notice, true)
		} else if m.record.Status == store.StatusPaused {
			m.notice = "Computer sleep detected; tournament clock was already paused."
		}
		return m.refreshClock()
	}
	warnMinute := false
	levelChime := false
	warningSegment := 0
	clockAdvanced := false
	if m.record.Status == store.StatusRunning && delta > 0 {
		before := m.record.Checkpoint
		warningSegment = before.SegmentIndex
		warnMinute = shouldPlayMinuteWarning(before, delta, m.record.Config.Schedule, m.summary)
		levelChime = shouldPlayLevelChangeSound(before, delta, m.record.Config.Schedule, m.summary)
		transitions, err := m.service.AdvanceClock(context.Background(), delta, wall)
		if err != nil {
			m.clockPersistenceFailed = true
			m.setStatus(err, true)
			m.refreshClock()
			return false
		}
		clockAdvanced = true
		if levelChime {
			m.playLevelChangeSound()
		}
		if len(transitions) > 0 {
			m.notice = transitionNotice(transitions, m.record.Config.Schedule)
		}
	}
	if !m.refreshClock() {
		return clockAdvanced
	}
	if warnMinute && m.record.Status == store.StatusRunning && m.record.Checkpoint.SegmentIndex == warningSegment && m.record.Checkpoint.Remaining > 0 && m.record.Checkpoint.Remaining <= time.Minute+time.Second {
		if soundErr := sound.PlayMinuteWarningDesktop(m.prefs.MinuteSoundFile, m.prefs.SoundEnabled, m.onAsyncError); soundErr != nil {
			m.setStatus(soundErr, true)
		}
	}
	return true
}

func (m *Model) onAsyncError(err error) {
	if m.asyncError != nil && err != nil {
		m.asyncError(err)
	}
}

func (m *Model) refreshClock() bool {
	if m.database == nil || m.service == nil {
		return true
	}
	record, err := m.database.Tournament(context.Background())
	if err != nil {
		m.setStatus(err, true)
		return false
	}
	m.record = record
	return true
}

func upcomingLevelAvailable(checkpoint store.Checkpoint, schedule []config.Segment, summary tournament.Summary) bool {
	index := checkpoint.SegmentIndex + 1
	if checkpoint.PendingIndex > 0 {
		index = checkpoint.PendingIndex
	}
	if index < 1 || index > len(schedule) || schedule[index-1].Kind != config.SegmentLevel {
		return false
	}
	available, _, _, err := domain.LevelAvailability(schedule[index-1], summary.TotalStack, summary.ActivePlayers)
	return err == nil && available
}

func (m *Model) playLevelChangeSound() {
	if soundErr := sound.PlayDesktop(m.prefs.SoundFile, m.prefs.SoundEnabled, m.onAsyncError); soundErr != nil {
		m.setStatus(soundErr, true)
	}
}

func shouldPlayMinuteWarning(checkpoint store.Checkpoint, delta time.Duration, schedule []config.Segment, summary tournament.Summary) bool {
	if !checkpoint.Running || checkpoint.HoldReason != "" || checkpoint.SegmentFinished || !upcomingLevelAvailable(checkpoint, schedule, summary) {
		return false
	}
	threshold := time.Minute + time.Second // allow MP3 startup latency so sound begins at 60 seconds
	return checkpoint.Remaining > threshold && checkpoint.Remaining-delta <= threshold
}

func shouldPlayLevelChangeSound(checkpoint store.Checkpoint, delta time.Duration, schedule []config.Segment, summary tournament.Summary) bool {
	if !checkpoint.Running || checkpoint.HoldReason != "" || checkpoint.SegmentFinished || !upcomingLevelAvailable(checkpoint, schedule, summary) {
		return false
	}
	return checkpoint.Remaining > time.Second && checkpoint.Remaining-delta <= time.Second
}

func (m *Model) syncClock() {
	if m.service == nil {
		return
	}
	now := time.Now()
	if m.lastTick.IsZero() {
		m.resetClockAnchor(now)
		return
	}
	wallNow := stripMonotonic(now)
	if m.processClockDelta(now.Sub(m.lastTick), wallNow.Sub(m.lastWall), now) {
		m.lastTick, m.lastWall = now, wallNow
		if m.clockPersistenceFailed {
			m.setStatus("Clock checkpoint recovered.", false)
		}
		m.clockPersistenceFailed = false
	}
}

func (m *Model) resetClockAnchor(now time.Time) {
	m.lastTick = now
	m.lastWall = stripMonotonic(now)
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
	_, finalErr := m.service.LatestFinalResult(context.Background())
	m.finalResultAvailable = finalErr == nil
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

func (m *Model) onKey(key keyEvent) bool {
	if m.clockPersistenceFailed {
		m.syncClock()
		if m.clockPersistenceFailed {
			canDismiss := key.Type == keyEsc || key.Type == keyRunes && strings.EqualFold(key.String(), "q")
			if m.modal != nil && m.modal.confirm == confirmQuit {
				canDismiss = canDismiss || key.Type == keyEnter || key.Type == keyRunes && (strings.EqualFold(key.String(), "y") || strings.EqualFold(key.String(), "n"))
			}
			if m.modal != nil && m.modal.kind == dialogMessage && key.Type == keyEnter {
				canDismiss = true
			}
			if !canDismiss {
				m.setStatus("Clock checkpoint failed; retrying before accepting actions.", true)
				return false
			}
		}
	}
	if m.modal != nil {
		if key.Type == keyRunes && strings.EqualFold(key.String(), "q") && m.modal.kind != dialogText {
			m.suspendedModal = m.modal
			m.modal = nil
			m.askQuit()
			return false
		}
		return m.handleDialogKey(key)
	}
	if m.browser != nil {
		if key.Type == keyRunes && strings.EqualFold(key.String(), "q") {
			m.askQuit()
			return false
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

func (m *Model) handleStartupKey(key keyEvent) bool {
	switch key.Type {
	case keyUp, keyLeft:
		m.startupCursor = (m.startupCursor + 2) % 3
	case keyDown, keyRight:
		m.startupCursor = (m.startupCursor + 1) % 3
	case keyEnter:
		switch m.startupCursor {
		case 0:
			m.modal = &dialog{kind: dialogText, title: "New tournament", prompt: "Tournament name", textPurpose: textTournamentName}
		case 1:
			m.startBrowser(browseTournamentDB, screenStartup)
		case 2:
			m.askQuit()
		}
	case keyEsc:
		m.askQuit()
	case keyRunes:
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
	return false
}

func (m *Model) handleMainKey(key keyEvent) bool {
	if key.Type == keySpace {
		m.toggleClock()
		return false
	}
	if key.Type == keyEsc {
		return false
	}
	if key.Type != keyRunes {
		return false
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
		choices := []string{"Add player", "Player info / history", "Rename player", "Remove player", "Void purchase"}
		if m.record.Status == store.StatusEnded {
			choices = []string{"Player info / history"}
		}
		m.modal = &dialog{kind: dialogMenu, purpose: menuPlayers, title: "Players", choices: choices}
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
	case "?":
		m.screen = screenRules
	case "n", "N":
		m.toggleSound()
	case "f", "F":
		m.showFinalResults()
	}
	return false
}

func (m *Model) handleRulesKey(key keyEvent) bool {
	switch key.Type {
	case keySpace:
		m.toggleClock()
	case keyEsc:
		m.screen = screenMain
	case keyRunes:
		if key.String() == "?" {
			m.screen = screenMain
		} else if strings.EqualFold(key.String(), "q") {
			m.askQuit()
		}
	}
	return false
}

func (m *Model) handleBrowserKey(key keyEvent) bool {
	switch key.Type {
	case keyEsc:
		m.browser = nil
		m.screen = m.browserCancel
	case keyUp:
		m.browser.move(-1)
	case keyDown:
		m.browser.move(1)
	case keyHome:
		m.browser.cursor = 0
	case keyEnd:
		m.browser.cursor = len(m.browser.entries) - 1
	case keyBackspace:
		m.browser.directory = filepath.Dir(m.browser.directory)
		if err := m.browser.reload(); err != nil {
			m.setStatus(err, true)
		}
	case keyEnter:
		path, done, err := m.browser.enter()
		if err != nil {
			m.setStatus(err, true)
			return false
		}
		if done {
			m.browserSelected(path)
		}
	case keyRunes:
		switch strings.ToLower(key.String()) {
		case "n":
			if m.browser.purpose == browseSaveDatabase || m.browser.purpose == browseSaveTemplate {
				m.modal = &dialog{kind: dialogText, title: "New file", prompt: "Filename (Enter for " + m.defaultFilename() + ")", textPurpose: textBrowserFilename}
			}
		case "h":
			if err := m.browser.toggleHidden(); err != nil {
				m.setStatus(err, true)
			}
		case "u":
			if err := m.browser.toggleUnsupported(); err != nil {
				m.setStatus(err, true)
			}
		}
	}
	return false
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
	case browseLevelSound:
		m.prefs.SoundFile = path
		if err := preferences.Save(m.prefs); err != nil {
			m.setStatus(err, true)
		} else {
			m.setStatus("Custom end-of-level sound saved.", false)
		}
	case browseMinuteSound:
		m.prefs.MinuteSoundFile = path
		if err := preferences.Save(m.prefs); err != nil {
			m.setStatus(err, true)
		} else {
			m.setStatus("Custom one-minute warning sound saved.", false)
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

func (m *Model) completeNewTemplateChoice() {
	template, err := config.DefaultTemplate()
	if err != nil {
		m.showMessage("Default template failed", err.Error())
		return
	}
	m.pendingTemplate = template
	m.startBrowser(browseSaveDatabase, screenStartup)
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
	if kind == tournament.PurchaseAddOn {
		if !m.record.Config.Registration.AddOnsEnabled {
			m.showMessage("Add-on unavailable", tournament.ErrAddOnsDisabled.Error())
			return
		}
		if !tournament.AddOnWindowOpen(m.record.Config, m.record.Checkpoint.SegmentIndex, m.record.Checkpoint.SegmentFinished) {
			m.showMessage("Add-on unavailable", tournament.ErrAddOnWindowClosed.Error())
			return
		}
	}
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
			if player.HasInitialBuyIn && player.Active && !player.HasAddOn {
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
	currentSegmentIndex := m.record.Checkpoint.SegmentIndex - 1
	if currentSegmentIndex >= len(m.record.Config.Schedule) {
		currentSegmentIndex = len(m.record.Config.Schedule) - 1
	}
	if currentSegmentIndex < 0 {
		currentSegmentIndex = -1
	}
	for index, segment := range m.record.Config.Schedule {
		if segment.Kind == config.SegmentLevel {
			level++
			choices[index] = fmt.Sprintf("Level %d — %d/%d", level, segment.SmallBlind, segment.BigBlind)
			if index == currentSegmentIndex {
				choices[index] += " · CURRENT"
			}
		} else {
			breakNo++
			label := segment.Label
			if label == "" {
				label = fmt.Sprintf("Break %d", breakNo)
			}
			choices[index] = label
			if index == currentSegmentIndex {
				choices[index] += " · CURRENT"
			}
		}
	}
	cursor := currentSegmentIndex
	if cursor < 0 {
		cursor = 0
	}
	m.modal = &dialog{kind: dialogSelectSegment, title: "Select level or break", choices: choices, cursor: cursor}
}

func (m *Model) openSettings() {
	choices := []string{
		"Export reusable template", "Import/edit YAML template", "Rename tournament",
		"Choose end-of-level sound", "Choose one-minute warning sound",
		"Use default end-of-level sound", "Use default one-minute warning sound",
	}
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
			m.resetClockAnchor(time.Now())
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

func (m *Model) finishQuit() bool {
	if err := m.closeTournament(); err != nil {
		m.showMessage("Exit warning", err.Error())
		return false
	}
	return true
}

func (m *Model) showMessage(title, message string) {
	if messageIsError(title) {
		message = formatUserError(message)
	}
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
		if isError {
			m.statusMessage = formatUserError(m.statusMessage)
		}
	case string:
		m.statusMessage = message
		if isError {
			m.statusMessage = formatUserError(m.statusMessage)
		}
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
