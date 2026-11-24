package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/timkimtim/YourPet/internal/pet"
)

const className = "YourPet.Overlay"
const (
	commandLoad = 1 + iota
	commandDefault
	commandNextIdle
	commandAutoWalk
	commandTopmost
	commandHideFullscreen
	commandAnimateFullscreen
	commandClickThrough
	commandStartup
	commandVisibility
	commandReposition
	commandAbout
	commandUpdate
	commandQuit
	commandSize      = 30
	commandRadius    = 40
	commandAnimation = 100
)

var sizes = []int{50, 75, 100, 125, 150}
var radii = []int{0, 150, 260, 420}
var instance *app

type settings struct {
	Scale             int
	Radius            int
	Topmost           bool
	HideFullscreen    bool
	AnimateFullscreen bool
	ClickThrough      bool
	AutoWalk          bool
	Placed            bool
	X, Y              int32
}

type app struct {
	window, icon                                                uintptr
	taskbarCreated                                              uint32
	configDir, executable                                       string
	settings                                                    settings
	player                                                      *pet.Player
	imported                                                    bool
	x, y                                                        float64
	surface                                                     surface
	lastCell                                                    pet.Cell
	lastFadeFrom                                                pet.Cell
	lastFadeAlpha                                               uint8
	drawn                                                       bool
	drawnAt                                                     point
	hidden, dragDown, dragging, dialogOpen, menuOpen, trayAdded bool
	windowShown, fullscreen                                     bool
	exclusiveFullscreen, portalActive                           bool
	fullscreenCheckAt                                           time.Time
	fullscreenMotion                                            pet.FullscreenMotion
	fullscreenScan                                              fullscreenScan
	dragOrigin, dragOffset, dragLast                            point
	dragDirection                                               int
	dragMovedAt                                                 time.Time
	displayError                                                bool
	updates                                                     updateState
}

func main() {
	runtime.LockOSThread()
	lang, _, _ := getUILanguage.Call()
	russianUI = lang&0x03ff == 0x19
	if len(os.Args) == 4 && os.Args[1] == applyUpdateArg {
		if err := runUpdateHelper(os.Args[2], os.Args[3]); err != nil {
			showMessage(0, tr("Could not install the update", "Не удалось установить обновление")+"\n\n"+err.Error(), 0x10)
		}
		return
	}
	context := ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	setDPIContext.Call(context)
	name := utf16("Local\\YourPet.Instance")
	mutex, _, lastError := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if mutex != 0 {
		defer closeHandle.Call(mutex)
		if lastError == syscall.Errno(183) {
			wnd, _, _ := findWindow.Call(uintptr(unsafe.Pointer(utf16(className))), 0)
			if wnd != 0 {
				postMessage.Call(wnd, wmShowPet, 0, 0)
			}
			return
		}
	}
	if err := run(); err != nil {
		showMessage(0, err.Error(), 0x10)
	}
}

func run() error {
	folder, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	a := &app{
		configDir: filepath.Join(folder, "YourPet"), executable: executable, hidden: true,
		settings: settings{Scale: 100, Radius: 260, Topmost: true, HideFullscreen: true, AnimateFullscreen: true, AutoWalk: true},
	}
	instance = a
	a.loadSettings()
	pkg, _, customErr := pet.ReadFile(a.packagePath())
	if customErr == nil {
		a.imported = true
	} else {
		pkg, err = a.defaultPackage()
		if err != nil {
			return fmt.Errorf("пакет по умолчанию: %w", err)
		}
	}
	if err = a.makeWindow(); err != nil {
		return err
	}
	defer a.surface.close()
	defer a.tray(2)
	a.tray(0)
	if !a.trayAdded {
		a.settings.ClickThrough = false
	}
	a.setPet(pkg)
	a.refreshStyle()
	a.setVisible(true)
	timer, _, err := setTimer.Call(a.window, 1, 32, 0)
	if timer == 0 {
		return fmt.Errorf("не удалось запустить анимацию: %w", err)
	}
	defer killTimer.Call(a.window, 1)
	a.updates.nextCheck = time.Now().Add(15 * time.Second)
	if issue := completeUpdateStartup(executable, os.Args[1:]); issue != "" {
		a.updates.nextCheck = time.Now().Add(updateCheckInterval)
		a.updateMessage(tr("The update failed. YourPet restarted the previous version.",
			"Обновление не установилось. YourPet запустил предыдущую версию.")+"\n\n"+issue, 0x30)
	}
	if customErr != nil && !errors.Is(customErr, os.ErrNotExist) {
		showMessage(a.window, "Сохранённый питомец не загрузился. Запущен питомец по умолчанию.\n\n"+customErr.Error(), 0x30)
	}
	for {
		msg := message{}
		result, _, err := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(result) {
		case -1:
			return err
		case 0:
			return nil
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (a *app) makeWindow() error {
	module, _, _ := getModuleHandle.Call(0)
	cursor, _, _ := loadCursor.Call(0, 32512) // IDC_ARROW
	a.icon, _, _ = loadIcon.Call(module, 1)
	if a.icon == 0 {
		a.icon, _, _ = loadIcon.Call(0, 32512) // IDI_APPLICATION fallback.
	}
	name := utf16(className)
	class := windowClass{
		Size: uint32(unsafe.Sizeof(windowClass{})), Style: 0x0008, // CS_DBLCLKS
		Proc: syscall.NewCallback(windowProc), Instance: module, Icon: a.icon, Cursor: cursor, Name: name, SmallIcon: a.icon,
	}
	if value, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); value == 0 {
		return err
	}
	start, _ := cursorPosition()
	area := workArea(start)
	hwnd, _, err := createWindow.Call(wsExLayered|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(utf16("YourPet"))), 0x80000000,
		uintptr(area.Right-230), uintptr(area.Bottom-240), 192, 208, 0, 0, module, 0)
	runtime.KeepAlive(class)
	if hwnd == 0 {
		return err
	}
	a.window = hwnd
	msg, _, _ := registerWindowMessage.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	a.taskbarCreated = uint32(msg)
	return nil
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	a := instance
	if a == nil || a.window == 0 || hwnd != a.window {
		result, _, _ := defaultWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	if message == a.taskbarCreated && message != 0 {
		a.tray(0)
		if !a.trayAdded && (a.hidden || a.settings.ClickThrough) {
			a.settings.ClickThrough = false
			a.refreshStyle()
			a.setVisible(true)
		}
		return 0
	}
	switch message {
	case wmDestroy:
		a.tray(2)
		postQuitMessage.Call(0)
	case wmClose:
		destroyWindow.Call(hwnd)
	case wmTimer:
		a.tick()
		a.pollUpdates()
	case wmShowPet:
		a.setVisible(true)
	case wmDisplayChange, wmDPICHanged:
		if !a.dragDown {
			a.clamp(a.monitor(), false)
		}
		a.drawn = false
		a.fullscreenCheckAt = time.Time{}
	case wmLButtonDown, wmLButtonDoubleClick:
		a.beginDrag()
	case wmLButtonUp:
		a.finishLeftButton()
	case wmMouseMove:
		a.moveDrag()
	case wmCaptureChanged, wmCancelMode:
		a.endDrag()
	case wmRButtonUp:
		a.openMenu()
	case wmTray:
		switch uint32(lParam) & 0xffff {
		case wmRButtonUp:
			a.openMenu()
		case wmLButtonUp, wmLButtonDoubleClick:
			a.setVisible(true)
		case ninBalloonUserClick:
			if a.updates.available != nil {
				a.offerUpdate()
			}
		}
	default:
		result, _, _ := defaultWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	return 0
}

func (a *app) loadSettings() {
	data, err := os.ReadFile(filepath.Join(a.configDir, "settings.json"))
	if err != nil {
		return
	}
	loaded := a.settings
	if json.Unmarshal(data, &loaded) != nil {
		return
	}
	if loaded.Scale < 50 || loaded.Scale > 150 {
		loaded.Scale = 100
	}
	if loaded.Radius < 0 || loaded.Radius > 800 {
		loaded.Radius = 260
	}
	a.settings = loaded
}

func (a *app) writeLocal(name string, data []byte) error {
	if err := os.MkdirAll(a.configDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(a.configDir, ".yourpet-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	ok, _, err := moveFile.Call(uintptr(unsafe.Pointer(utf16(file.Name()))),
		uintptr(unsafe.Pointer(utf16(filepath.Join(a.configDir, name)))), 0x9)
	if ok == 0 {
		return err
	}
	return nil
}

func (a *app) saveSettings() {
	a.settings.X, a.settings.Y = int32(math.Round(a.x)), int32(math.Round(a.y))
	a.settings.Placed = true
	data, err := json.MarshalIndent(a.settings, "", "  ")
	if err == nil {
		err = a.writeLocal("settings.json", data)
	}
	if err != nil {
		a.error(tr("Could not save settings", "Настройки не сохранились"), err)
	}
}

func (a *app) packagePath() string {
	return filepath.Join(a.configDir, "current.pet")
}

func (a *app) defaultPackage() (*pet.Package, error) {
	return pet.Decode(bundledPet)
}

func (a *app) importPackage() bool {
	a.dialogOpen = true
	defer func() { a.dialogOpen = false }()
	filename, err := choosePackage(a.window)
	if err != nil {
		a.error(tr("Could not open the file picker", "Не удалось открыть диалог"), err)
		return false
	}
	if filename == "" {
		return false
	}
	pkg, data, err := pet.ReadFile(filename)
	if err != nil {
		a.error(tr("Could not load the pet", "Не удалось загрузить питомца"), err)
		return false
	}
	if err = a.writeLocal("current.pet", data); err != nil {
		a.error(tr("Could not save the pet", "Не удалось сохранить питомца"), err)
		return false
	}
	a.imported = true
	a.setPet(pkg)
	return true
}

func (a *app) restoreDefault() {
	pkg, err := a.defaultPackage()
	if err != nil {
		a.error(tr("Default pet", "Питомец по умолчанию"), err)
		return
	}
	if err = os.Remove(a.packagePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.error(tr("Could not restore the default pet", "Не удалось восстановить Рыжика"), err)
		return
	}
	a.imported = false
	a.setPet(pkg)
}

func (a *app) setPet(pkg *pet.Package) {
	area := a.monitor()
	oldWidth, oldHeight := a.size()
	initial := a.player == nil
	now := time.Now()
	a.player = pet.NewPlayer(pkg, now, now.UnixNano())
	a.fullscreenMotion = pet.FullscreenMotion{}
	a.setPortalActive(false)
	if initial {
		if a.settings.Placed {
			a.x, a.y = float64(a.settings.X), float64(a.settings.Y)
			area = a.monitor()
			a.clamp(area, false)
		} else {
			a.placeInCorner(area)
		}
	} else {
		w, h := a.size()
		a.x += float64(oldWidth-w) / 2
		a.y += float64(oldHeight - h)
		a.clamp(area, false)
	}
	a.drawn = false
	a.displayError = false
	a.tray(1)
}

func (a *app) size() (int, int) {
	if a.player == nil {
		return 192, 208
	}
	dpi, _, _ := getWindowDPI.Call(a.window)
	if dpi == 0 {
		dpi = 96
	}
	factor := float64(a.settings.Scale) * float64(dpi) / 9600
	return max(16, int(math.Round(float64(a.player.Pet.CellWidth)*factor))),
		max(16, int(math.Round(float64(a.player.Pet.CellHeight)*factor)))
}

func (a *app) monitor() rectangle {
	if a.player == nil && !a.settings.Placed {
		pos, _ := cursorPosition()
		return workArea(pos)
	}
	if a.player == nil {
		return workArea(point{a.settings.X, a.settings.Y})
	}
	w, h := a.size()
	return workArea(point{int32(a.x) + int32(w/2), int32(a.y) + int32(h/2)})
}

func (a *app) clamp(area rectangle, partial bool) {
	w, h := a.size()
	minX, minY := float64(area.Left), float64(area.Top)
	maxX, maxY := float64(area.Right)-float64(w), float64(area.Bottom)-float64(h)
	if partial {
		minX -= float64(w) / 2
		maxX += float64(w) / 2
		minY -= float64(h) / 2
		maxY += float64(h) / 2
	}
	a.x = math.Max(minX, math.Min(a.x, maxX))
	a.y = math.Max(minY, math.Min(a.y, maxY))
}

func (a *app) placeInCorner(area rectangle) {
	w, h := a.size()
	a.x = float64(area.Right) - float64(w) - 24
	a.y = float64(area.Bottom) - float64(h) - 18
	a.clamp(area, false)
}

func (a *app) beginDrag() {
	pos, ok := cursorPosition()
	if !ok || a.player == nil {
		return
	}
	a.dragDown = true
	a.dragOrigin, a.dragLast = pos, pos
	a.dragOffset = point{pos.X - int32(a.x), pos.Y - int32(a.y)}
	setCapture.Call(a.window)
}

func dragThresholdReached(origin, pos point) bool {
	return math.Hypot(float64(pos.X-origin.X), float64(pos.Y-origin.Y)) >= 4
}

func (a *app) moveDrag() {
	if !a.dragDown {
		return
	}
	pos, ok := cursorPosition()
	if !ok {
		return
	}
	if !a.dragging && !dragThresholdReached(a.dragOrigin, pos) {
		return
	}
	a.dragging = true
	a.x, a.y = float64(pos.X-a.dragOffset.X), float64(pos.Y-a.dragOffset.Y)
	a.clamp(workArea(pos), true)
	if pos.X > a.dragLast.X {
		a.dragDirection = 1
	}
	if pos.X < a.dragLast.X {
		a.dragDirection = -1
	}
	if pos != a.dragLast {
		a.dragMovedAt = time.Now()
	}
	a.dragLast = pos
	a.tick()
}

func (a *app) finishLeftButton() {
	if !a.dragDown {
		return
	}
	pos, ok := cursorPosition()
	click := !a.dragging && (!ok || !dragThresholdReached(a.dragOrigin, pos))
	a.endDrag()
	releaseCapture.Call()
	if click && !a.hidden && a.player != nil {
		a.player.Jump(time.Now())
		a.tick()
	}
}

func (a *app) endDrag() {
	if a.dragDown {
		a.fullscreenCheckAt = time.Time{}
	}
	dragged := a.dragging
	a.dragDown, a.dragging = false, false
	a.dragDirection = 0
	if dragged {
		a.clamp(a.monitor(), false)
		a.saveSettings()
	}
}

func (a *app) tick() {
	if a.hidden || a.player == nil || a.dialogOpen || a.menuOpen {
		return
	}
	now := time.Now()
	w, h := a.size()
	if a.handleFullscreen(now, w, h) {
		return
	}
	dx, dy := math.Inf(1), math.Inf(1)
	radius := float64(a.settings.Radius)
	if look := a.player.Pet.Look; look != nil {
		if pos, ok := cursorPosition(); ok {
			dx = float64(pos.X) - a.x - float64(look.Anchor[0])*float64(w)/float64(a.player.Pet.CellWidth)
			dy = float64(pos.Y) - a.y - float64(look.Anchor[1])*float64(h)/float64(a.player.Pet.CellHeight)
			if dpi, _, _ := getWindowDPI.Call(a.window); dpi != 0 {
				radius *= float64(dpi) / 96
			}
		}
	}
	area := a.monitor()
	direction := a.dragDirection
	if now.Sub(a.dragMovedAt) > 140*time.Millisecond {
		direction = 0
	}
	pose := a.player.Tick(pet.Input{
		Now: now, CursorX: dx, CursorY: dy, LookRadius: radius, AutoWalk: a.settings.AutoWalk,
		Dragging: a.dragging, DragDirection: direction,
		CanWalkLeft: a.x > float64(area.Left)+1, CanWalkRight: a.x+float64(w) < float64(area.Right)-1,
	})
	if pose.Move != 0 {
		a.x += pose.Move * float64(a.settings.Scale) / 100
		a.clamp(area, false)
	}
	a.showPose(pose, w, h)
}

func (a *app) showPose(pose pet.Pose, w, h int) {
	if err := a.paint(pose, w, h); err != nil {
		if !a.displayError {
			a.displayError = true
			a.error(tr("Could not display the pet", "Не удалось отобразить питомца"), err)
		}
		return
	}
	if !a.windowShown {
		a.windowShown = true
		showWindow.Call(a.window, 4) // SW_SHOWNOACTIVATE
	}
}

func (a *app) paint(pose pet.Pose, width, height int) error {
	pos := point{int32(math.Round(a.x)), int32(math.Round(a.y))}
	resized := a.surface.width != width || a.surface.height != height
	changed := !a.drawn || resized || a.lastCell != pose.Cell ||
		a.lastFadeFrom != pose.FadeFrom || a.lastFadeAlpha != pose.FadeAlpha
	if !changed && a.drawnAt == pos {
		return nil
	}
	if resized {
		if err := a.surface.resize(width, height); err != nil {
			return err
		}
	}
	if changed {
		pkg := a.player.Pet
		a.surface.draw(pkg.AtlasImage, pkg.Rect(pose.Cell), pkg.Rect(pose.FadeFrom), pose.FadeAlpha)
	}
	if err := a.surface.present(a.window, pos); err != nil {
		return err
	}
	a.drawn, a.lastCell, a.drawnAt = true, pose.Cell, pos
	a.lastFadeFrom, a.lastFadeAlpha = pose.FadeFrom, pose.FadeAlpha
	return nil
}

func (a *app) setVisible(visible bool) {
	if visible {
		a.hidden = false
		a.fullscreenCheckAt = time.Time{}
		a.tick()
	} else if a.trayAdded {
		a.hidden = true
		a.fullscreenMotion = pet.FullscreenMotion{}
		a.setPortalActive(false)
		a.hideOverlay()
	}
}

func (a *app) hideOverlay() {
	if a.windowShown {
		a.windowShown = false
		showWindow.Call(a.window, 0)
		if a.dragDown {
			a.endDrag()
			releaseCapture.Call()
		}
	}
}

func (a *app) refreshStyle() {
	exStyle, _, _ := getWindowLong.Call(a.window, ^uintptr(19)) // GWL_EXSTYLE
	if a.settings.ClickThrough || a.portalActive {
		exStyle |= wsExTransparent
	} else {
		exStyle &^= wsExTransparent
	}
	setWindowLong.Call(a.window, ^uintptr(19), exStyle)
	order := ^uintptr(1) // HWND_NOTOPMOST
	if a.settings.Topmost {
		order = ^uintptr(0)
	}
	setWindowPos.Call(a.window, order, 0, 0, 0, 0, 0x0001|0x0002|0x0010|0x0020)
}

func (a *app) tray(action uintptr) {
	if a.window == 0 || action != 0 && !a.trayAdded {
		return
	}
	data := iconData{
		Size: uint32(unsafe.Sizeof(iconData{})), Window: a.window, ID: 1, Flags: 7, Callback: wmTray, Icon: a.icon,
	}
	label := "YourPet"
	if a.player != nil {
		label += " — " + a.player.Pet.Name
	}
	copy(data.Tip[:127], syscall.StringToUTF16(label))
	ok, _, _ := notifyIcon.Call(action, uintptr(unsafe.Pointer(&data)))
	if action == 0 {
		a.trayAdded = ok != 0
	}
	if action == 2 {
		a.trayAdded = false
	}
}

func (a *app) error(title string, err error) {
	showMessage(a.window, title+"\n\n"+err.Error(), 0x10)
}
