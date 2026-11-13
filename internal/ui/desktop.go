package ui

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// BuildVersion is set from the release tag when building release binaries.
var BuildVersion = "dev"

// RunDesktop launches the Fyne desktop UI. The tournament model and persistence
// workflows remain shared with the existing tested application logic.
func RunDesktop(application fyne.App) error {
	if application == nil {
		return fmt.Errorf("Fyne application is required")
	}
	application.Settings().SetTheme(newSpacedustTheme())
	icon := pokerClockIcon()
	application.SetIcon(icon)
	window := application.NewWindow("Poker Clock")
	window.SetIcon(icon)
	window.SetPadded(false)
	window.SetFixedSize(false)
	window.Resize(fyne.NewSize(1280, 800))

	controller := &desktop{app: application, window: window, model: NewModel()}
	controller.model.asyncError = func(err error) {
		fyne.Do(func() {
			if controller.closed.Load() {
				return
			}
			controller.model.setStatus(err, true)
			controller.render(true)
		})
	}
	controller.keys = newDesktopKeySink(controller)
	controller.model.lastTick = time.Now()
	controller.model.lastWall = stripMonotonic(controller.model.lastTick)
	controller.render(true)
	window.Canvas().SetOnTypedRune(controller.dispatchRune)
	window.Canvas().SetOnTypedKey(controller.dispatchKeyEvent)
	window.SetCloseIntercept(controller.requestQuit)
	window.SetFullScreen(true)
	window.Show()
	fyne.Do(func() {
		controller.debugf("startup display: fullscreen=%t scale=%.2f size=%v FYNE_SCALE=%q", window.FullScreen(), window.Canvas().Scale(), window.Canvas().Size(), os.Getenv("FYNE_SCALE"))
	})

	stop := make(chan struct{})
	defer close(stop)
	go controller.clockLoop(stop)
	application.Run()
	controller.closed.Store(true)
	if controller.model.database != nil {
		if err := controller.model.closeTournament(); err != nil {
			return err
		}
	}
	return nil
}

type desktop struct {
	app              fyne.App
	window           fyne.Window
	model            *Model
	home             *desktopHome
	dualWindow       fyne.Window
	dualHome         *desktopHome
	dualKeys         *desktopKeySink
	dualTitle        string
	dualConfigKey    string
	dualFullscreen   *widget.Button
	dualClose        *widget.Button
	dualVisible      bool
	entry            *desktopEntry
	keys             *desktopKeySink
	browserScroll    *container.Scroll
	modalScroll      *container.Scroll
	rulesScroll      *container.Scroll
	startupButtons   []*widget.Button
	browserButtons   []*widget.Button
	lastScreen       screen
	lastHasDB        bool
	lastModal        bool
	skipResolverRune rune
	closed           atomic.Bool
}

func (d *desktop) clockLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			fyne.Do(func() {
				if d.closed.Load() {
					return
				}
				modalBeforeTick := d.model.modal
				d.model.onClockTick()
				d.refreshDualClock()
				if d.model.modal != modalBeforeTick {
					d.render(true)
					return
				}
				if d.model.screen == screenMain && d.home != nil {
					d.home.refreshClock(d.model)
					if d.model.modal != nil {
						d.disableBackgroundControls()
					}
				}
			})
		}
	}
}

func (d *desktop) render(_ bool) {
	if d.closed.Load() {
		return
	}
	m := d.model
	if m.modal == nil && m.browser == nil && m.screen == screenMain && d.home != nil && d.lastScreen == m.screen && d.lastHasDB == (m.database != nil) && !d.lastModal {
		d.home.dualVisible = d.dualVisible
		d.home.refresh(m)
		d.refreshDualDisplay()
		d.window.Canvas().Focus(d.keys)
		return
	}

	d.entry = nil
	d.home = nil
	d.browserScroll = nil
	d.modalScroll = nil
	d.rulesScroll = nil
	d.startupButtons = nil
	d.browserButtons = nil
	var content fyne.CanvasObject
	switch {
	case m.modal != nil:
		base := d.baseView()
		d.entry = nil
		d.disableBackgroundControls()
		content = d.modalOverlay(base, m.modal)
	case m.browser != nil:
		content = d.browserView()
		d.home = nil
	case m.screen == screenRules:
		content = d.rulesView()
		d.home = nil
	case m.screen == screenEditor:
		content = d.editorView()
		d.home = nil
	case m.screen == screenResolver:
		content = d.handResolverView()
		d.home = nil
	case m.screen == screenStartup || m.database == nil:
		content = d.startupView()
		d.home = nil
	default:
		var home fyne.CanvasObject
		home, d.home = d.mainView()
		content = home
	}

	d.window.SetContent(container.NewBorder(d.keys, nil, nil, nil, container.New(windowSideInsetLayout{inset: 16}, content)))
	if d.browserScroll != nil {
		d.scrollBrowserSelection()
	}
	if d.modalScroll != nil && m.modal != nil {
		d.scrollModalSelection(m.modal.cursor)
	}
	d.lastScreen, d.lastHasDB = m.screen, m.database != nil
	d.lastModal = m.modal != nil
	d.refreshDualDisplay()
	if d.entry != nil {
		d.window.Canvas().Focus(d.entry)
	} else {
		d.window.Canvas().Focus(d.keys)
	}
}

func (d *desktop) baseView() fyne.CanvasObject {
	if d.model.browser != nil {
		return d.browserView()
	}
	if d.model.screen == screenMain && d.model.database != nil {
		content, home := d.mainView()
		d.home = home
		return content
	}
	if d.model.screen == screenRules {
		return d.rulesView()
	}
	if d.model.screen == screenResolver {
		return d.handResolverView()
	}
	if d.model.screen == screenEditor {
		return d.editorView()
	}
	return d.startupView()
}

func (d *desktop) disableBackgroundControls() {
	if d.home != nil {
		for _, button := range d.home.actions {
			button.Disable()
		}
		if d.home.fullscreen != nil {
			d.home.fullscreen.Disable()
		}
	}
	for _, button := range d.startupButtons {
		button.Disable()
	}
	for _, button := range d.browserButtons {
		button.Disable()
	}
}

type windowSideInsetLayout struct{ inset float32 }

func (l windowSideInsetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	minimum := objects[0].MinSize()
	return fyne.NewSize(minimum.Width+2*l.inset, minimum.Height)
}

func (l windowSideInsetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	width := size.Width - 2*l.inset
	if width < 0 {
		width = 0
	}
	objects[0].Move(fyne.NewPos(l.inset, 0))
	objects[0].Resize(fyne.NewSize(width, size.Height))
}

func (d *desktop) scrollBrowserSelection() {
	if d.browserScroll == nil || d.model.browser == nil {
		return
	}
	rowHeight := float32(48)
	visibleRows := int(d.browserScroll.Size().Height / rowHeight)
	if visibleRows < 1 {
		visibleRows = 1
	}
	firstVisible := d.model.browser.cursor - visibleRows + 1
	if firstVisible < 0 {
		firstVisible = 0
	}
	d.browserScroll.ScrollToOffset(fyne.NewPos(0, float32(firstVisible)*rowHeight))
}

func (d *desktop) scrollModalSelection(cursor int) {
	if d.modalScroll == nil {
		return
	}
	rowHeight := float32(48)
	visibleRows := int(d.modalScroll.Size().Height / rowHeight)
	if visibleRows < 1 {
		visibleRows = 1
	}
	firstVisible := cursor - visibleRows + 1
	if firstVisible < 0 {
		firstVisible = 0
	}
	d.modalScroll.ScrollToOffset(fyne.NewPos(0, float32(firstVisible)*rowHeight))
}

func (d *desktop) scrollRulesPage(direction float32) {
	if d.rulesScroll != nil {
		d.scrollRules(direction * d.rulesScroll.Size().Height * 0.9)
	}
}

func (d *desktop) scrollRules(delta float32) {
	if d.rulesScroll != nil {
		d.scrollRulesTo(d.rulesScroll.Offset.Y + delta)
	}
}

func (d *desktop) scrollRulesTo(offset float32) {
	if d.rulesScroll == nil {
		return
	}
	maxOffset := d.rulesScroll.Content.MinSize().Height - d.rulesScroll.Size().Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset < 0 {
		offset = 0
	} else if offset > maxOffset {
		offset = maxOffset
	}
	d.rulesScroll.ScrollToOffset(fyne.NewPos(0, offset))
}

func (d *desktop) scrollRulesToEnd() {
	if d.rulesScroll != nil {
		d.scrollRulesTo(d.rulesScroll.Content.MinSize().Height - d.rulesScroll.Size().Height)
	}
}

func (d *desktop) dispatchKeyEvent(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	if event.Name != fyne.KeyPlus && event.Name != fyne.KeyMinus {
		d.skipResolverRune = 0
	}
	if event.Name == fyne.KeyF11 {
		d.debugf("F11 event: screen=%d focused=%T fullscreen=%t dual=%t scale=%.2f", d.model.screen, d.window.Canvas().Focused(), d.window.FullScreen(), d.dualVisible, d.window.Canvas().Scale())
	}
	if d.model.screen == screenEditor && d.model.browser == nil && (d.model.modal == nil || d.model.modal.kind == dialogEditorForm) {
		switch event.Name {
		case fyne.KeyF11:
			d.toggleFullScreen()
			return
		case fyne.KeyEscape:
			d.dispatchKey(keyEvent{Type: keyEsc})
			return
		default:
			return
		}
	}
	if d.model.screen == screenRules {
		switch event.Name {
		case fyne.KeyUp, fyne.KeyLeft:
			d.scrollRules(-24)
			return
		case fyne.KeyDown, fyne.KeyRight:
			d.scrollRules(24)
			return
		case fyne.KeyPageUp:
			d.scrollRulesPage(-1)
			return
		case fyne.KeyPageDown:
			d.scrollRulesPage(1)
			return
		case fyne.KeyHome:
			d.scrollRulesTo(0)
			return
		case fyne.KeyEnd:
			d.scrollRulesToEnd()
			return
		}
	}
	switch event.Name {
	case fyne.KeySpace:
		d.dispatchKey(keyEvent{Type: keySpace})
	case fyne.KeyEscape:
		d.dispatchKey(keyEvent{Type: keyEsc})
	case fyne.KeyUp:
		d.dispatchKey(keyEvent{Type: keyUp})
	case fyne.KeyDown:
		d.dispatchKey(keyEvent{Type: keyDown})
	case fyne.KeyHome:
		d.dispatchKey(keyEvent{Type: keyHome})
	case fyne.KeyEnd:
		d.dispatchKey(keyEvent{Type: keyEnd})
	case fyne.KeyLeft:
		d.dispatchKey(keyEvent{Type: keyLeft})
	case fyne.KeyRight:
		d.dispatchKey(keyEvent{Type: keyRight})
	case fyne.KeyReturn, fyne.KeyEnter:
		d.dispatchKey(keyEvent{Type: keyEnter})
	case fyne.KeyBackspace, fyne.KeyDelete:
		d.dispatchKey(keyEvent{Type: keyBackspace})
	case fyne.KeyPlus, fyne.KeyEqual:
		if d.model.screen == screenResolver {
			d.skipResolverRune = '+'
			d.dispatchKey(keyEvent{Type: keyRunes, Runes: []rune{'+'}})
		}
	case fyne.KeyMinus:
		if d.model.screen == screenResolver {
			d.skipResolverRune = '-'
			d.dispatchKey(keyEvent{Type: keyRunes, Runes: []rune{'-'}})
		}
	case fyne.KeyF11:
		d.toggleFullScreen()
	}
}

func (d *desktop) toggleDualDisplay() {
	if d.model.screen != screenMain || d.model.database == nil || d.model.modal != nil || d.model.browser != nil {
		return
	}
	if d.dualVisible {
		d.hideDualDisplay()
		return
	}
	d.window.SetFullScreen(false)
	if d.dualWindow != nil {
		d.dualWindow.SetFullScreen(false)
	}
	if d.dualWindow == nil {
		d.dualWindow = d.app.NewWindow("Poker Clock")
		d.dualWindow.SetIcon(pokerClockIcon())
		d.dualWindow.SetPadded(false)
		d.dualWindow.SetFixedSize(false)
		d.dualWindow.Resize(fyne.NewSize(1280, 900))
		content, home := d.dualView()
		d.dualHome = home
		d.dualConfigKey = fmt.Sprintf("%#v", d.model.record.Config)
		d.dualKeys = newDualDisplayKeySink(d)
		d.dualWindow.SetContent(container.NewBorder(d.dualKeys, nil, nil, nil, content))
		d.dualWindow.SetCloseIntercept(func() { d.hideDualDisplay() })
		d.dualWindow.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
			if event != nil && event.Name == fyne.KeyF11 {
				d.toggleFullScreen()
			}
		})
		d.dualWindow.Canvas().SetOnTypedRune(func(value rune) {
			if strings.EqualFold(string(value), "c") {
				d.hideDualDisplay()
			}
		})
	}
	d.dualVisible = true
	d.dualWindow.Show()
	d.dualWindow.Canvas().Focus(d.dualKeys)
	d.refreshDualDisplay()
	d.render(true)
}

func (d *desktop) hideDualDisplay() {
	if d.dualWindow == nil || !d.dualVisible {
		return
	}
	d.dualWindow.SetFullScreen(false)
	d.dualWindow.Hide()
	d.window.RequestFocus()
	d.dualVisible = false
	d.render(true)
}

func (d *desktop) toggleFullScreen() {
	fullScreen := !d.window.FullScreen()
	d.debugf("fullscreen request: current=%t target=%t focused=%T dual=%t", !fullScreen, fullScreen, d.window.Canvas().Focused(), d.dualVisible)
	d.window.SetFullScreen(fullScreen)
	if d.dualVisible {
		d.dualWindow.SetFullScreen(fullScreen)
	}
	if d.model.modal == nil && d.model.browser == nil && d.model.screen != screenEditor {
		d.window.Canvas().Focus(d.keys)
		if d.dualVisible && d.dualKeys != nil {
			d.dualWindow.Canvas().Focus(d.dualKeys)
		}
	}
}

func (d *desktop) debugf(format string, args ...any) {
	if os.Getenv("POKER_CLOCK_DEBUG") != "" {
		log.Printf("poker-clock ui: "+format, args...)
	}
}

func (d *desktop) refreshDualDisplay() {
	if d.dualVisible && d.dualHome != nil {
		configKey := fmt.Sprintf("%#v", d.model.record.Config)
		if configKey != d.dualConfigKey {
			wasFocused := d.dualWindow.Canvas().Focused() == d.dualKeys
			content, home := d.dualView()
			d.dualHome, d.dualConfigKey = home, configKey
			d.dualWindow.SetContent(container.NewBorder(d.dualKeys, nil, nil, nil, content))
			if wasFocused {
				d.dualWindow.Canvas().Focus(d.dualKeys)
			}
		}
		title := "Poker Clock — " + d.model.record.Name
		if title != d.dualTitle {
			d.dualWindow.SetTitle(title)
			d.dualTitle = title
		}
		d.dualHome.refresh(d.model)
	}
}

func (d *desktop) refreshDualClock() {
	if d.dualVisible && d.dualHome != nil {
		d.dualHome.refreshClock(d.model)
	}
}

func (d *desktop) advanceWelcomeSelection() {
	d.model.startupCursor = (d.model.startupCursor + 1) % 5
	d.render(true)
}

func (d *desktop) newButton(label string, tapped func()) *widget.Button {
	return widget.NewButton(label, tapped)
}

func (d *desktop) dispatchRune(value rune) {
	if d.model.screen == screenResolver && d.skipResolverRune == value {
		d.skipResolverRune = 0
		return
	}
	if value == 0 || d.model.screen == screenEditor && d.model.browser == nil && (d.model.modal == nil || d.model.modal.kind == dialogEditorForm || d.model.modal.kind == dialogText) {
		return
	}
	d.dispatchKey(keyEvent{Type: keyRunes, Runes: []rune{value}})
}

func (d *desktop) dispatchKey(key keyEvent) {
	if d.closed.Load() {
		return
	}
	if key.Type == keyRunes && d.model.screen == screenMain && d.model.modal == nil && d.model.browser == nil {
		if strings.EqualFold(key.String(), "d") {
			d.toggleDualDisplay()
			return
		}
		if strings.EqualFold(key.String(), "c") && d.dualVisible {
			d.hideDualDisplay()
			return
		}
	}
	if d.model.onKey(key) {
		d.app.Quit()
		return
	}
	d.render(true)
}

func (d *desktop) requestQuit() {
	if d.closed.Load() {
		return
	}
	d.model.askQuit()
	d.render(true)
}

func (d *desktop) closeModal() {
	d.dispatchKey(keyEvent{Type: keyEsc})
}

func (d *desktop) clickChoice(index, _ int) {
	if d.model.modal != nil {
		d.model.modal.cursor = index
	}
	d.dispatchKey(keyEvent{Type: keyEnter})
}

func (d *desktop) activate(key rune) {
	d.dispatchKey(keyEvent{Type: keyRunes, Runes: []rune{key}})
}

var _ fyne.Focusable = (*desktopKeySink)(nil)
var _ fyne.Tabbable = (*desktopKeySink)(nil)

type desktopKeySink struct {
	widget.BaseWidget
	owner    *desktop
	dispatch func(rune)
	key      func(*fyne.KeyEvent)
}

func newDesktopKeySink(owner *desktop) *desktopKeySink {
	keys := &desktopKeySink{owner: owner, dispatch: owner.dispatchRune, key: owner.dispatchKeyEvent}
	keys.ExtendBaseWidget(keys)
	return keys
}

func newDualDisplayKeySink(owner *desktop) *desktopKeySink {
	keys := &desktopKeySink{owner: owner}
	keys.dispatch = func(value rune) {
		if strings.EqualFold(string(value), "c") {
			owner.hideDualDisplay()
		}
	}
	keys.key = func(event *fyne.KeyEvent) {
		if event != nil && event.Name == fyne.KeyF11 {
			owner.toggleFullScreen()
		}
	}
	keys.ExtendBaseWidget(keys)
	return keys
}

func (k *desktopKeySink) CreateRenderer() fyne.WidgetRenderer {
	return &desktopKeySinkRenderer{}
}
func (k *desktopKeySink) TypedRune(value rune) {
	if k.dispatch != nil {
		k.dispatch(value)
	}
}
func (k *desktopKeySink) TypedKey(event *fyne.KeyEvent) {
	if event != nil && event.Name == fyne.KeyTab {
		if k.owner != nil && k.owner.model.modal == nil {
			switch k.owner.model.screen {
			case screenStartup:
				k.owner.advanceWelcomeSelection()
			case screenResolver:
				k.owner.dispatchKey(keyEvent{Type: keyTab})
			}
		}
		return
	}
	if k.key != nil {
		k.key(event)
	}
}
func (*desktopKeySink) FocusGained()     {}
func (*desktopKeySink) FocusLost()       {}
func (*desktopKeySink) AcceptsTab() bool { return true }

type desktopKeySinkRenderer struct{}

func (*desktopKeySinkRenderer) Layout(fyne.Size)             {}
func (*desktopKeySinkRenderer) MinSize() fyne.Size           { return fyne.NewSize(0, 0) }
func (*desktopKeySinkRenderer) Refresh()                     {}
func (*desktopKeySinkRenderer) Objects() []fyne.CanvasObject { return nil }
func (*desktopKeySinkRenderer) Destroy()                     {}

func (d *desktop) refreshHome() {
	if d.home != nil {
		d.home.refresh(d.model)
	}
}
