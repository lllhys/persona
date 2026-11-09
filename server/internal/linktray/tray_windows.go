//go:build windows

package linktray

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Options are how the program starts the tray.
type Options struct {
	Host Host
	// Exe is the program that starts the tray at sign-in and opens pairing links: the console-less
	// persona-link-service.exe the install put in place.
	Exe string
	// Open is a link given on the command line (`tray --open <link>`, what Windows runs for a pairing link).
	Open string
	// Lang overrides Windows' language ("" follows it).
	Lang i18n.Lang
}

const (
	className    = "PersonaLinkTray"
	panelWidth   = 360 // DIP: the panel's width, as tokens.css's --size-panel
	panelGuess   = 420 // DIP: its height until the page has said
	panelMargin  = 12  // DIP: between the panel and the taskbar or the screen's edge
	pairWidth    = 560
	pairHeight   = 680
	pairMinW     = 480
	pairMinH     = 520
	tickEvery    = time.Second
	taskEvery    = 30 * time.Second
	clickGrace   = 300 * time.Millisecond // a click on the icon right after the panel closed was what closed it
	offlineAfter = time.Minute            // a link down this long gets a notification
)

// command is what a second start of the tray, or `persona-link install` / `uninstall`, sends to the running one.
type command struct {
	Open bool   `json:"open,omitempty"` // show the panel
	Pair string `json:"pair,omitempty"` // a link to put in the pairing window
	Quit bool   `json:"quit,omitempty"`
}

type app struct {
	o     Options
	host  Host
	lang  i18n.Lang
	hwnd  uintptr
	class *uint16
	icon  notifyIcon

	taskbarCreated uint32
	calls          chan func()

	snap     Snapshot
	view     View
	task     Task
	taskAt   time.Time
	taskBusy bool

	iconKey string // look, size and taskbar theme of the icon shown

	panel         *webWindow
	panelPending  bool // created or opened, waiting for the page's size before showing
	panelHiddenAt time.Time
	panelHeight   int32 // DIP, from the page

	pair     *webWindow
	pairJSON string

	awaitingNotified bool
	downSince        time.Time
	downNotified     bool
	connectedOnce    bool
	ticks            int
}

var current *app // the one tray of this process: the window procedure finds it here

// instance is what tells this data directory's tray from another's on the same computer (as link.PokeEventName).
func instance(dataDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(dataDir)))
	return hex.EncodeToString(sum[:8])
}

// Send hands c to the tray running for dataDir; false when none runs.
func Send(dataDir string, c command) bool {
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr(className))), uintptr(unsafe.Pointer(utf16Ptr(instance(dataDir)))))
	if hwnd == 0 {
		return false
	}
	b, _ := json.Marshal(c)
	cds := copyDataStruct{Data: 1, Bytes: uint32(len(b)), Ptr: uintptr(unsafe.Pointer(&b[0]))}
	r, _, _ := procSendMessageW.Call(hwnd, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)))
	runtime.KeepAlive(b)
	return r != 0
}

// Quit asks the tray running for dataDir to go, and waits up to timeout for it to have gone: `install` replaces the
// program it runs from, `uninstall` removes it. False when one is still there.
func Quit(dataDir string, timeout time.Duration) bool {
	if !Send(dataDir, command{Quit: true}) {
		return true
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if !Running(dataDir) {
			return true
		}
	}
	return !Running(dataDir)
}

// Running says whether a tray runs for dataDir.
func Running(dataDir string) bool {
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(utf16Ptr(className))), uintptr(unsafe.Pointer(utf16Ptr(instance(dataDir)))))
	return hwnd != 0
}

// Run is the tray: the icon, the panel, the pairing window, until the user quits it or it is told to. A second
// one for the same data directory hands its link (or "show the panel") to the first and returns.
func Run(o Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dataDir := o.Host.DataDir()
	mutex, err := windows.CreateMutex(nil, false, utf16Ptr(`Local\PersonaLinkTray.`+instance(dataDir)))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(mutex)
		c := command{Open: o.Open == "", Pair: o.Open}
		if !Send(dataDir, c) {
			return errors.New("another Persona Link tray is starting; try again in a moment")
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(mutex)
	if procSetProcessDpiAwarenessContext.Find() == nil {
		procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	}

	a := &app{o: o, host: o.Host, lang: o.Lang, calls: make(chan func(), 64), panelHeight: panelGuess}
	if a.lang == "" {
		a.lang = uiLanguage()
	}
	current = a
	if err := a.createWindow(dataDir); err != nil {
		return err
	}
	a.taskbarCreated = registerMessage("TaskbarCreated")
	a.icon = notifyIcon{hwnd: a.hwnd, id: 1}
	a.refresh(true)
	if !a.icon.add() {
		log.Printf("tray: the notification area did not take the icon; trying again when Explorer is back")
	}
	procSetTimer.Call(a.hwnd, 1, uintptr(tickEvery/time.Millisecond), 0)
	stop := a.watchDir(dataDir)
	defer stop()
	if o.Open != "" {
		a.openPair(o.Open, "")
	}

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	a.icon.remove()
	return nil
}

func registerMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16Ptr(name))))
	return uint32(r)
}

var wndProcCallback = syscall.NewCallback(wndProc)

// createWindow makes the tray's own window: never shown, it gets the icon's clicks, the timer, the theme changes
// and other programs' commands (found by its class and title, the data directory's instance).
func (a *app) createWindow(dataDir string) error {
	a.class = utf16Ptr(className)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{WndProc: wndProcCallback, ClassName: a.class, Cursor: cursor}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("register window class: %w", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(wsExToolWindow, uintptr(unsafe.Pointer(a.class)),
		uintptr(unsafe.Pointer(utf16Ptr(instance(dataDir)))), wsPopup, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return fmt.Errorf("create window: %w", err)
	}
	a.hwnd = hwnd
	return nil
}

func wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	a := current
	if a == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), w, l)
		return r
	}
	if r, handled := a.handle(hwnd, m, w, l); handled {
		return r
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

func loword(v uintptr) uint32 { return uint32(v) & 0xFFFF }
func hiword(v uintptr) uint32 { return (uint32(v) >> 16) & 0xFFFF }

func (a *app) handle(hwnd uintptr, m uint32, w, l uintptr) (uintptr, bool) {
	isPanel := a.panel != nil && hwnd == a.panel.hwnd
	isPair := a.pair != nil && hwnd == a.pair.hwnd
	switch {
	case m == wmTrayIcon:
		switch loword(l) {
		case ninSelect, ninKeySelect:
			a.togglePanel()
		case wmContextMenu:
			a.showMenu(int32(int16(loword(w))), int32(int16(hiword(w))))
		case ninBalloonUserClik:
			a.openPanel()
		}
		return 0, true
	case m == a.taskbarCreated && m != 0:
		a.iconKey = "" // a new taskbar may be another color or size
		a.updateIcon()
		a.icon.add()
		return 0, true
	case m == wmRun:
		for {
			select {
			case f := <-a.calls:
				f()
			default:
				return 0, true
			}
		}
	case m == wmTimer && hwnd == a.hwnd:
		a.tick()
		return 0, true
	case m == wmSettingChange || m == wmDwmColorChanged:
		if hwnd == a.hwnd {
			a.themeChanged()
		}
		return 0, false
	case m == wmCopyData && hwnd == a.hwnd:
		cds := (*copyDataStruct)(fromLParam(l))
		if cds.Data != 1 || cds.Bytes == 0 || cds.Bytes > 64<<10 {
			return 0, true
		}
		var c command
		if json.Unmarshal(unsafe.Slice((*byte)(fromLParam(cds.Ptr)), cds.Bytes), &c) != nil {
			return 0, true
		}
		a.later(func() { a.command(c) }) // not inside the sender's SendMessage
		return 1, true
	case m == wmActivate && isPanel:
		if loword(w) == waInactive && a.panel.visible() {
			a.panel.hide()
			a.panelHiddenAt = time.Now()
		}
		return 0, false
	case m == wmSize && (isPanel || isPair):
		web := a.panel
		if isPair {
			web = a.pair
		}
		if web.web != nil {
			web.web.Resize()
		}
		return 0, false
	case m == wmDpiChanged && isPair:
		r := (*rect)(fromLParam(l))
		a.pair.setScale(float64(loword(w)) / 96)
		a.pair.place(r.Left, r.Top, r.width(), r.height(), swpNoActivate)
		return 0, true
	case m == wmGetMinMaxInfo && isPair:
		mm := (*minMaxInfo)(fromLParam(l))
		scale := float64(dpiForWindow(hwnd)) / 96
		mm.MinTrackSize = point{int32(pairMinW * scale), int32(pairMinH * scale)}
		return 0, true
	case m == wmClose && isPair:
		a.closePair()
		return 0, true
	case m == wmDestroy && hwnd == a.hwnd:
		procPostQuitMessage.Call(0)
		return 0, true
	}
	return 0, false
}

// later runs f on the UI thread, after what it is doing now.
func (a *app) later(f func()) {
	a.calls <- f
	postMessage(a.hwnd, wmRun, 0, 0)
}

// localize is err as the tray says it (ErrorText); nil stays nil.
func (a *app) localize(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(ErrorText(a.lang, err))
}

// fromPage handles a page's messages after WebView2's message event has returned: WebView2 holds back its other
// callbacks until then, a new window's WebView being ready among them, so the pairing window could not open from
// the panel.
func (a *app) fromPage(handle func(request)) func(request) {
	return func(r request) { a.later(func() { handle(r) }) }
}

// background runs work off the UI thread and then done on it with work's result.
func background[T any](a *app, work func() (T, error), done func(T, error)) {
	go func() {
		v, err := work()
		a.later(func() { done(v, err) })
	}()
}

func (a *app) command(c command) {
	switch {
	case c.Quit:
		a.quit()
	case c.Pair != "":
		a.openPair(c.Pair, "")
	case c.Open:
		a.openPanel()
	}
}

func (a *app) quit() {
	procKillTimer.Call(a.hwnd, 1)
	if a.panel != nil {
		a.panel.destroy()
		a.panel = nil
	}
	a.closePair()
	a.icon.remove()
	procDestroyWindow.Call(a.hwnd)
}

// tick: every second while a window is open (for "connected for …" and countdowns), every third otherwise; the
// scheduled task every half minute.
func (a *app) tick() {
	a.ticks++
	open := (a.panel != nil && a.panel.visible()) || a.pair != nil
	if open || a.ticks%3 == 0 {
		a.refresh(false)
	}
}

// refresh reads the state again and shows it wherever it is shown. forceTask asks the scheduled task too.
func (a *app) refresh(forceTask bool) {
	if (forceTask || time.Since(a.taskAt) > taskEvery) && !a.taskBusy {
		a.taskBusy = true
		background(a, func() (Task, error) { return a.host.TaskState(), nil }, func(t Task, _ error) {
			a.task, a.taskAt, a.taskBusy = t, time.Now(), false
			a.refresh(false)
		})
	}
	s := a.host.Snapshot()
	s.Task = a.task
	s.Autostart = Autostart()
	if s.Now.IsZero() {
		s.Now = time.Now()
	}
	a.snap = s
	a.view = Build(a.lang, s)
	a.updateIcon()
	a.notifications()
	if a.panel != nil && a.panel.visible() {
		a.pushView()
	}
	if a.pair != nil {
		a.pushPair()
	}
}

// changed is refresh after an action of the user's: the panel's page gets the new view even while hidden (a switch
// flipped from the menu, an action that ended after the panel closed), so it never opens showing the old one.
func (a *app) changed() {
	a.refresh(true)
	if a.panel != nil && !a.panel.visible() {
		a.pushView()
	}
}

func (a *app) pushView() {
	a.panel.send(map[string]any{"type": "view", "view": a.view})
}

func (a *app) pushPair() {
	ps := BuildPair(a.lang, a.snap)
	b, _ := json.Marshal(ps)
	if string(b) == a.pairJSON {
		return // redrawing would take the cursor out of what the user is typing
	}
	a.pairJSON = string(b)
	a.pair.send(map[string]any{"type": "pair", "pair": ps})
}

// updateIcon draws the icon for the look, at the notification area's size, for the taskbar's color.
func (a *app) updateIcon() {
	dpi := dpiForWindow(a.hwnd)
	size := smallIconSize(dpi)
	dark := systemDark()
	key := fmt.Sprintf("%s/%d/%v", a.view.Look, size, dark)
	if key == a.iconKey && a.icon.tip == a.view.Tooltip {
		return
	}
	icon := a.icon.icon
	if key != a.iconKey {
		img, err := RenderIcon(a.view.Look, size, dark)
		if err != nil {
			log.Printf("tray: icon: %v", err)
			return
		}
		icon = hicon(img)
		a.iconKey = key
	}
	a.icon.set(icon, a.view.Tooltip)
}

func smallIconSize(dpi uint32) int {
	if procGetSystemMetricsForDpi.Find() == nil {
		if r, _, _ := procGetSystemMetricsForDpi.Call(smCxSmIcon, uintptr(dpi)); r > 0 {
			return int(r)
		}
	}
	return int(16 * dpi / 96)
}

// notifications: once when the hub starts waiting for the user's OK, once when a link that was up has been down a
// minute. Nothing at sign-in before the link was ever up: the network may still be coming.
func (a *app) notifications() {
	look := a.view.Look
	if look == LookAwaitingConfirm {
		if !a.awaitingNotified {
			n := AwaitingNotification(a.lang)
			a.icon.balloon(n.Title, n.Body)
			a.awaitingNotified = true
		}
	} else {
		a.awaitingNotified = false
	}
	switch look {
	case LookConnected, LookListening, LookAwaitingConfirm, LookNodeMissing:
		a.connectedOnce, a.downSince, a.downNotified = true, time.Time{}, false
	case LookDown:
		if a.downSince.IsZero() {
			a.downSince = time.Now()
		}
		if a.connectedOnce && !a.downNotified && time.Since(a.downSince) >= offlineAfter {
			n := OfflineNotification(a.lang)
			a.icon.balloon(n.Title, n.Body)
			a.downNotified = true
		}
	}
}

func (a *app) themeChanged() {
	a.iconKey = ""
	a.updateIcon()
	if a.panel != nil {
		a.panel.themeChanged()
	}
	if a.pair != nil {
		a.pair.themeChanged()
	}
}

// watchDir refreshes as soon as something in the data directory changes (link.status.json, link.json), instead of
// waiting for the next tick. stop ends the watch.
func (a *app) watchDir(dir string) (stop func()) {
	h, err := windows.FindFirstChangeNotification(dir, false, windows.FILE_NOTIFY_CHANGE_LAST_WRITE|windows.FILE_NOTIFY_CHANGE_FILE_NAME)
	if err != nil {
		return func() {}
	}
	quit, _ := windows.CreateEvent(nil, 1, 0, nil)
	go func() {
		defer windows.FindCloseChangeNotification(h)
		for {
			r, err := windows.WaitForMultipleObjects([]windows.Handle{h, quit}, false, windows.INFINITE)
			if err != nil || r != windows.WAIT_OBJECT_0 {
				return
			}
			time.Sleep(150 * time.Millisecond) // a write and its rename are one change
			if windows.FindNextChangeNotification(h) != nil {
				return
			}
			a.later(func() { a.refresh(false) })
		}
	}()
	return func() { windows.SetEvent(quit) }
}

// --- the panel

func (a *app) togglePanel() {
	switch {
	case a.panel != nil && a.panel.visible():
		a.panel.hide()
		a.panelHiddenAt = time.Now()
	case time.Since(a.panelHiddenAt) < clickGrace:
		// The click took the focus from the panel, which closed it: that was the user closing it.
	default:
		a.openPanel()
	}
}

func (a *app) openPanel() {
	if !webViewInstalled() {
		a.icon.balloon("Persona Link", i18n.T(a.lang, "link-tray.no_webview"))
		r, _ := a.icon.rect()
		a.showMenu(r.Left, r.Top)
		return
	}
	a.refresh(true)
	if a.panel == nil {
		html, err := Page("panel", a.lang, Boot{Theme: themeFor(systemDark(), backdropAvailable()), Version: a.host.Version(), View: &a.view})
		if err != nil {
			log.Printf("tray: panel: %v", err)
			return
		}
		a.panel, err = newWebWindow(a.class, a.hwnd, true, "Persona Link", panelWidth, panelGuess, html, a.fromPage(a.panelMessage))
		if err != nil {
			log.Printf("tray: panel: %v", err)
			a.panel = nil
			return
		}
		a.panelPending = true
		// Shown when the page says how tall it is; a page that never does is shown anyway.
		time.AfterFunc(2*time.Second, func() { a.later(a.showPanelNow) })
		return
	}
	a.pushView()
	a.showPanelNow()
}

func (a *app) showPanelNow() {
	if a.panel == nil {
		return
	}
	a.panelPending = false
	a.placePanel()
	a.panel.show()
	a.panel.send(map[string]any{"type": "show"})
}

// placePanel puts the panel by the icon, inside the work area, on the taskbar's side; sized for that monitor's DPI.
func (a *app) placePanel() {
	icon, haveIcon := a.icon.rect()
	var mon uintptr
	if haveIcon {
		mon, _, _ = procMonitorFromRect.Call(uintptr(unsafe.Pointer(&icon)), monitorNearest)
	} else {
		p := cursorPos()
		icon = rect{p.X, p.Y, p.X + 1, p.Y + 1}
		mon, _, _ = procMonitorFromPoint.Call(uintptr(uint32(p.X))|uintptr(uint32(p.Y))<<32, monitorNearest)
	}
	mi, ok := monitorInfoOf(mon)
	if !ok {
		return
	}
	scale := float64(dpiForMonitor(mon)) / 96
	a.panel.setScale(scale)
	w, h := int32(panelWidth*scale), int32(float64(a.panelHeight)*scale)
	margin := int32(panelMargin * scale)
	work, full := mi.Work, mi.Monitor
	clamp := func(v, lo, hi int32) int32 { return max(lo, min(v, hi)) }
	x := clamp((icon.Left+icon.Right)/2-w/2, work.Left+margin, work.Right-w-margin)
	y := work.Bottom - h - margin
	switch {
	case work.Top > full.Top: // taskbar on top
		y = work.Top + margin
	case work.Left > full.Left: // on the left
		x = work.Left + margin
	case work.Right < full.Right: // on the right
		x = work.Right - w - margin
	}
	a.panel.place(x, y, w, h, 0)
}

// panelMessage is what the panel's page asks.
func (a *app) panelMessage(r request) {
	switch r.Cmd {
	case "size":
		var s struct{ Height float64 }
		if json.Unmarshal(r.Args, &s) == nil && s.Height > 0 {
			a.panelHeight = int32(s.Height + 0.5)
			if a.panelPending {
				a.showPanelNow()
			} else if a.panel.visible() {
				a.placePanel()
			}
		}
	case "escape":
		a.panel.hide()
		a.panelHiddenAt = time.Now()
	case "action":
		var act struct {
			ID string
			On bool
		}
		_ = json.Unmarshal(r.Args, &act)
		a.action(act.ID, act.On, func(err error) { a.panel.reply(r.ID, nil, a.localize(err)) })
	case "rename":
		var in struct{ Name string }
		_ = json.Unmarshal(r.Args, &in)
		w := a.panel
		background(a, func() (string, error) { return a.host.Rename(in.Name) }, func(name string, err error) {
			if err == nil {
				a.notice(TonePositive, i18n.T(a.lang, "link-tray.renamed_ok", i18n.Args{"name": name}))
			}
			if w == a.panel && w != nil {
				w.reply(r.ID, map[string]string{"name": name}, a.localize(err))
			}
			a.refresh(false)
		})
	}
}

// notice shows the result of an action in the panel.
func (a *app) notice(tone, text string) {
	if a.panel != nil {
		a.panel.send(map[string]any{"type": "notice", "notice": map[string]string{"tone": tone, "text": text}})
	}
}

// action does what a row of the panel or the menu says. done is called on the UI thread with how it went.
func (a *app) action(id string, on bool, done func(error)) {
	finish := func(err error, ok string) {
		if err != nil {
			a.notice(ToneDanger, ErrorText(a.lang, err))
		} else if ok != "" {
			a.notice(TonePositive, ok)
		}
		a.changed()
		if done != nil {
			done(err)
		}
	}
	switch id {
	case ActReconnect:
		background(a, func() (struct{}, error) { return struct{}{}, a.host.Reconnect() }, func(_ struct{}, err error) { finish(err, "") })
	case ActRestart:
		background(a, func() (struct{}, error) { return struct{}{}, a.host.RestartService() }, func(_ struct{}, err error) {
			finish(err, i18n.T(a.lang, "link-tray.service_restarted"))
		})
	case ActInstall:
		background(a, func() (struct{}, error) { return struct{}{}, a.host.InstallService() }, func(_ struct{}, err error) {
			finish(err, i18n.T(a.lang, "link-tray.service_installed"))
		})
	case ActPair:
		if a.panel != nil {
			a.panel.hide()
		}
		a.openPair("", "")
		finish(nil, "")
	case ActLogs:
		finish(shellOpen(a.host.DataDir()), "")
	case ActAutostart:
		finish(SetAutostart(a.o.Exe, on), "")
	case ActQuit:
		if done != nil {
			done(nil)
		}
		a.quit()
	case ActPanel:
		a.openPanel()
	case ActRename: // the panel's own field (panel.js): from the menu, the panel opens with it
		a.openPanel()
		if a.panel != nil {
			a.panel.send(map[string]any{"type": "rename"})
		}
		finish(nil, "")
	}
}

// showMenu is the icon's context menu at (x, y): the native menu works without WebView2.
func (a *app) showMenu(x, y int32) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	labels := MenuLabels(a.lang)
	ids := []string{}
	add := func(flags uint32, text string, id string) {
		ids = append(ids, id)
		procAppendMenuW.Call(menu, uintptr(flags), uintptr(len(ids)), uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	add(mfString|mfGrayed, "Persona Link · "+a.view.Pill, "")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	add(mfString, labels[ActPanel], ActPanel)
	for _, act := range a.view.Actions {
		if act.ID == ActReconnect {
			flags := uint32(mfString)
			if act.Disabled {
				flags |= mfGrayed
			}
			add(flags, labels[ActReconnect], ActReconnect)
		}
	}
	add(mfString, labels[ActPair], ActPair)
	for _, act := range a.view.Actions {
		if act.ID == ActRename {
			add(mfString, labels[ActRename], ActRename)
		}
	}
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	auto := uint32(mfString)
	if a.snap.Autostart {
		auto |= mfChecked
	}
	add(auto, labels[ActAutostart], ActAutostart)
	add(mfString, labels[ActQuit], ActQuit)

	procSetForegroundWindow.Call(a.hwnd) // or the menu does not close when the user clicks elsewhere
	chosen, _, _ := procTrackPopupMenuEx.Call(menu, tpmReturn|tpmRightBtn, uintptr(x), uintptr(y), a.hwnd, 0)
	postMessage(a.hwnd, 0, 0, 0)
	if chosen == 0 || int(chosen) > len(ids) || ids[chosen-1] == "" {
		return
	}
	id := ids[chosen-1]
	a.action(id, !a.snap.Autostart, nil)
}

// --- the pairing window

func (a *app) openPair(prefill, tab string) {
	if !webViewInstalled() {
		a.icon.balloon("Persona Link", i18n.T(a.lang, "link-tray.no_webview"))
		return
	}
	a.refresh(false)
	if a.pair != nil {
		if prefill != "" {
			a.pair.send(map[string]any{"type": "prefill", "raw": prefill})
		}
		if tab != "" {
			a.pair.send(map[string]any{"type": "tab", "tab": tab})
		}
		a.pair.show()
		return
	}
	ps := BuildPair(a.lang, a.snap)
	b, _ := json.Marshal(ps)
	a.pairJSON = string(b)
	boot := Boot{Theme: themeFor(appsDark(), backdropAvailable()), Version: a.host.Version(), Pair: &ps, Prefill: prefill, Tab: tab}
	if prefill != "" {
		p := ParseLink(a.lang, prefill)
		boot.Preview = &p
	}
	html, err := Page("pair", a.lang, boot)
	if err != nil {
		log.Printf("tray: pair: %v", err)
		return
	}
	title := Labels(a.lang)["pair_window_title"]
	w, err := newWebWindow(a.class, 0, false, title, pairWidth, pairHeight, html, a.fromPage(a.pairMessage))
	if err != nil {
		log.Printf("tray: pair: %v", err)
		return
	}
	a.pair = w
	a.setWindowIcon(w.hwnd)
	// Centered on the monitor the pointer is on, at its DPI.
	p := cursorPos()
	mon, _, _ := procMonitorFromPoint.Call(uintptr(uint32(p.X))|uintptr(uint32(p.Y))<<32, monitorNearest)
	if mi, ok := monitorInfoOf(mon); ok {
		scale := float64(dpiForMonitor(mon)) / 96
		width, height := int32(pairWidth*scale), int32(min(float64(pairHeight)*scale, float64(mi.Work.height())*0.9))
		w.setScale(scale)
		w.place(mi.Work.Left+(mi.Work.width()-width)/2, mi.Work.Top+(mi.Work.height()-height)/2, width, height, 0)
	}
	w.show()
}

// setWindowIcon gives the pairing window the tray's mark in the title bar and Alt+Tab.
func (a *app) setWindowIcon(hwnd uintptr) {
	dpi := dpiForWindow(hwnd)
	for _, which := range []struct {
		kind uintptr
		size int
	}{{0, smallIconSize(dpi)}, {1, int(32 * dpi / 96)}} {
		theme := themeFor(appsDark(), false)
		ink := ToneColors[ToneInfo][0]
		if theme.Dark {
			ink = ToneColors[ToneInfo][1]
		}
		img, err := renderGlyph("hotspot", which.size, ink, iconStroke(which.size))
		if err != nil {
			return
		}
		procSendMessageW.Call(hwnd, wmSetIcon, which.kind, hicon(img))
	}
}

func (a *app) closePair() {
	if a.pair == nil {
		return
	}
	a.pair.destroy()
	a.pair = nil
	a.pairJSON = ""
}

// pairMessage is what the pairing window's page asks.
func (a *app) pairMessage(r request) {
	w := a.pair
	var args struct {
		Raw, Machine, Listen, Mode, URL string
	}
	_ = json.Unmarshal(r.Args, &args)
	reply := func(data any, err error) {
		if w == a.pair && w != nil {
			w.reply(r.ID, data, a.localize(err))
		}
		a.refresh(false)
	}
	switch r.Cmd {
	case "parse_link":
		w.reply(r.ID, ParseLink(a.lang, args.Raw), nil)
	case "pair":
		background(a, func() (string, error) { return a.host.Pair(args.Raw, args.Machine) }, func(machine string, err error) {
			if err != nil {
				reply(nil, err)
				return
			}
			reply(map[string]string{"text": i18n.T(a.lang, "link-tray.paired_ok", i18n.Args{"name": machine})}, nil)
		})
	case "pair_listen":
		background(a, func() (Invitation, error) { return a.host.PairListen(args.Listen) }, func(inv Invitation, err error) {
			if err == nil {
				inv.AddrText = CodeAddrs(a.lang, inv.Addrs)
				_, port, perr := net.SplitHostPort(inv.Listen)
				if perr != nil {
					port = strings.TrimPrefix(inv.Listen, ":")
				}
				inv.Firewall = FirewallCommand(port)
			}
			reply(inv, err)
		})
	case "set_mode":
		background(a, func() (struct{}, error) { return struct{}{}, a.host.SetMode(args.Mode) }, func(_ struct{}, err error) { reply(nil, err) })
	case "address_add":
		background(a, func() (struct{}, error) { return struct{}{}, a.host.AddAddress(args.Raw) }, func(_ struct{}, err error) { reply(nil, err) })
	case "address_remove":
		background(a, func() (struct{}, error) { return struct{}{}, a.host.RemoveAddress(args.URL) }, func(_ struct{}, err error) { reply(nil, err) })
	}
}
