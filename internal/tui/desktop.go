package tui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	tea "github.com/charmbracelet/bubbletea"
)

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
	controller.keys = newDesktopKeySink(controller)
	controller.model.lastTick = time.Now()
	controller.model.lastWall = stripMonotonic(controller.model.lastTick)
	controller.render(true)
	window.Canvas().SetOnTypedRune(controller.dispatchRune)
	window.Canvas().SetOnTypedKey(controller.dispatchKeyEvent)
	window.SetCloseIntercept(controller.requestQuit)
	window.Show()
	window.SetFullScreen(true)

	stop := make(chan struct{})
	defer close(stop)
	go controller.clockLoop(stop)
	application.Run()
	controller.closed = true
	if controller.model.database != nil {
		if err := controller.model.closeTournament(); err != nil {
			return err
		}
	}
	return nil
}

type desktop struct {
	app            fyne.App
	window         fyne.Window
	model          *Model
	home           *desktopHome
	entry          *desktopEntry
	keys           *desktopKeySink
	browserScroll  *container.Scroll
	modalScroll    *container.Scroll
	startupButtons []*widget.Button
	browserButtons []*widget.Button
	lastScreen     screen
	lastHasDB      bool
	closed         bool
}

func (d *desktop) clockLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case tick := <-ticker.C:
			fyne.Do(func() {
				if d.closed {
					return
				}
				d.model.onClockTick(tick)
				if d.model.screen == screenMain && d.home != nil {
					d.home.refresh(d.model)
					if d.model.modal != nil {
						d.disableBackgroundControls()
					}
				}
			})
		}
	}
}

func (d *desktop) render(force bool) {
	if d.closed {
		return
	}
	m := d.model
	if !force && m.modal == nil && m.browser == nil && m.screen == screenMain && d.home != nil && d.lastScreen == m.screen && d.lastHasDB == (m.database != nil) {
		d.home.refresh(m)
		return
	}

	d.entry = nil
	d.home = nil
	d.browserScroll = nil
	d.modalScroll = nil
	d.startupButtons = nil
	d.browserButtons = nil
	var content fyne.CanvasObject
	switch {
	case m.modal != nil:
		base := d.baseView()
		d.disableBackgroundControls()
		content = d.modalOverlay(base, m.modal)
	case m.browser != nil:
		content = d.browserView()
		d.home = nil
	case m.screen == screenRules:
		content = d.rulesView()
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

func (d *desktop) dispatchKeyEvent(event *fyne.KeyEvent) {
	if event == nil {
		return
	}
	switch event.Name {
	case fyne.KeySpace:
		d.dispatchTeaKey(tea.Key{Type: tea.KeySpace})
	case fyne.KeyEscape:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyEsc})
	case fyne.KeyUp:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyUp})
	case fyne.KeyDown:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyDown})
	case fyne.KeyHome:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyHome})
	case fyne.KeyEnd:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyEnd})
	case fyne.KeyLeft:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyLeft})
	case fyne.KeyRight:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyRight})
	case fyne.KeyReturn, fyne.KeyEnter:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyEnter})
	case fyne.KeyBackspace, fyne.KeyDelete:
		d.dispatchTeaKey(tea.Key{Type: tea.KeyBackspace})
	case fyne.KeyF11:
		d.window.SetFullScreen(!d.window.FullScreen())
	}
}

func (d *desktop) advanceWelcomeSelection() {
	d.model.startupCursor = (d.model.startupCursor + 1) % 3
	d.render(true)
}

func (d *desktop) newButton(label string, tapped func()) *widget.Button {
	return widget.NewButton(label, tapped)
}

func (d *desktop) dispatchRune(value rune) {
	if value == 0 {
		return
	}
	d.dispatchTeaKey(tea.Key{Type: tea.KeyRunes, Runes: []rune{value}})
}

func (d *desktop) dispatchTeaKey(key tea.Key) {
	if d.closed {
		return
	}
	_, command := d.model.onKey(tea.KeyMsg(key))
	if command != nil {
		d.app.Quit()
		return
	}
	d.render(true)
}

func (d *desktop) requestQuit() {
	if d.closed {
		return
	}
	d.model.askQuit()
	d.render(true)
}

func (d *desktop) closeModal() {
	d.dispatchTeaKey(tea.Key{Type: tea.KeyEsc})
}

func (d *desktop) clickChoice(index, cursor int) {
	if index < cursor {
		for i := cursor; i > index; i-- {
			d.model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyUp}))
		}
	} else {
		for i := cursor; i < index; i++ {
			d.model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyDown}))
		}
	}
	_, command := d.model.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	if command != nil {
		d.app.Quit()
		return
	}
	d.render(true)
}

func (d *desktop) activate(key rune) {
	d.dispatchTeaKey(tea.Key{Type: tea.KeyRunes, Runes: []rune{key}})
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
		if k.owner != nil && k.owner.model.screen == screenStartup && k.owner.model.modal == nil {
			k.owner.advanceWelcomeSelection()
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
