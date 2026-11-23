package main

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/timkimtim/YourPet/internal/pet"
)

var (
	foregroundWindow   = user32.NewProc("GetForegroundWindow")
	shellWindow        = user32.NewProc("GetShellWindow")
	windowClassName    = user32.NewProc("GetClassNameW")
	windowRect         = user32.NewProc("GetWindowRect")
	windowMinimized    = user32.NewProc("IsIconic")
	windowMaximized    = user32.NewProc("IsZoomed")
	windowVisible      = user32.NewProc("IsWindowVisible")
	windowMonitor      = user32.NewProc("MonitorFromWindow")
	enumWindows        = user32.NewProc("EnumWindows")
	dwmWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
	notificationState  = shell32.NewProc("SHQueryUserNotificationState")
	scanWindowCallback = syscall.NewCallback(scanWindow)
)

func (a *app) handleFullscreen(now time.Time, width, height int) bool {
	// Keep the pet available throughout a drag and check the destination on drop.
	if a.dragDown {
		return false
	}
	if !now.Before(a.fullscreenCheckAt) {
		center := point{int32(a.x) + int32(width/2), int32(a.y) + int32(height/2)}
		a.fullscreen, a.exclusiveFullscreen = false, false
		if a.settings.HideFullscreen {
			if fullscreen := a.fullscreenWindowAt(center); fullscreen != 0 {
				a.fullscreen = true
				foreground, _, _ := foregroundWindow.Call()
				a.exclusiveFullscreen = fullscreen == foreground && exclusiveGameActive()
			}
		}
		a.fullscreenCheckAt = now.Add(160 * time.Millisecond)
	}
	var leave, arrive *pet.Animation
	if a.settings.AnimateFullscreen {
		leave = a.player.Pet.Clips[pet.FullscreenHideID]
		arrive = a.player.Pet.Clips[pet.FullscreenShowID]
	}
	canLeave := a.windowShown && a.settings.Topmost && !a.exclusiveFullscreen
	display := a.fullscreenMotion.Step(now, a.fullscreen, canLeave, leave, arrive)
	a.setPortalActive(display.Animating)
	if display.Hidden {
		a.hideOverlay()
		return true
	}
	if display.Animating {
		a.showFrame(display.Cell, width, height)
		return true
	}
	return false
}

func (a *app) setPortalActive(active bool) {
	if a.portalActive != active {
		a.portalActive = active
		a.refreshStyle()
	}
}

func exclusiveGameActive() bool {
	const runningExclusiveD3D = 3 // QUNS_RUNNING_D3D_FULL_SCREEN
	var state int32
	result, _, _ := notificationState.Call(uintptr(unsafe.Pointer(&state)))
	return result == 0 && state == runningExclusiveD3D
}

type displayWindow struct {
	handle, monitor               uintptr
	bounds                        rectangle
	captionedMaximized, auxiliary bool
}

type fullscreenScan struct {
	monitor, shell, coveringWindow uintptr
	bounds                         rectangle
}

func (s *fullscreenScan) consider(window displayWindow) bool {
	if window.monitor != s.monitor {
		return false
	}
	if occupiesMonitor(window.bounds, s.bounds, window.captionedMaximized) {
		s.coveringWindow = window.handle
		return true
	}
	return !window.auxiliary
}

func (a *app) fullscreenWindowAt(at point) uintptr {
	monitor := monitorAt(at)
	if monitor == 0 {
		return 0
	}
	shell, _, _ := shellWindow.Call()
	a.fullscreenScan = fullscreenScan{monitor: monitor, shell: shell, bounds: displayInfo(monitor).Monitor}
	enumWindows.Call(scanWindowCallback, 0)
	return a.fullscreenScan.coveringWindow
}

// EnumWindows visits the desktop stack from front to back. Stop at the first
// relevant window on the pet's display, even if it isn't fullscreen.
func scanWindow(hwnd, _ uintptr) uintptr {
	scan := &instance.fullscreenScan
	if hwnd == instance.window || hwnd == scan.shell {
		return 1
	}
	if visible, _, _ := windowVisible.Call(hwnd); visible == 0 {
		return 1
	}
	if minimized, _, _ := windowMinimized.Call(hwnd); minimized != 0 {
		return 1
	}
	monitor, _, _ := windowMonitor.Call(hwnd, 0) // MONITOR_DEFAULTTONULL
	if monitor != scan.monitor {
		return 1
	}
	var cloaked uint32
	const dwmCloaked = 14
	if code, _, _ := dwmWindowAttribute.Call(hwnd, dwmCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked)); int32(code) >= 0 && cloaked != 0 {
		return 1
	}
	var className [64]uint16
	windowClassName.Call(hwnd, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	switch syscall.UTF16ToString(className[:]) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd", "#32768":
		return 1
	}
	var bounds rectangle
	// DWM bounds exclude the invisible resize border around an ordinary window.
	const dwmExtendedFrameBounds = 9
	code, _, _ := dwmWindowAttribute.Call(hwnd, dwmExtendedFrameBounds, uintptr(unsafe.Pointer(&bounds)), unsafe.Sizeof(bounds))
	if int32(code) < 0 || bounds.Right <= bounds.Left || bounds.Bottom <= bounds.Top {
		ok, _, _ := windowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		if ok == 0 {
			return 1
		}
	}
	if bounds.Right <= bounds.Left || bounds.Bottom <= bounds.Top {
		return 1
	}
	style, _, _ := getWindowLong.Call(hwnd, ^uintptr(15)) // GWL_STYLE
	maximized, _, _ := windowMaximized.Call(hwnd)
	captioned := style&0x00c00000 == 0x00c00000             // WS_CAPTION
	exStyle, _, _ := getWindowLong.Call(hwnd, ^uintptr(19)) // GWL_EXSTYLE
	if scan.consider(displayWindow{
		handle: hwnd, monitor: monitor, bounds: bounds, captionedMaximized: maximized != 0 && captioned,
		auxiliary: exStyle&(wsExToolWindow|wsExNoActivate) != 0,
	}) {
		return 0
	}
	return 1
}

func occupiesMonitor(window, monitor rectangle, captionedMaximized bool) bool {
	if captionedMaximized || monitor.Right <= monitor.Left || monitor.Bottom <= monitor.Top {
		return false
	}
	const tolerance = 2
	edges := [4]int32{
		window.Left - monitor.Left, window.Top - monitor.Top,
		window.Right - monitor.Right, window.Bottom - monitor.Bottom,
	}
	for _, delta := range edges {
		if delta < -tolerance || delta > tolerance {
			return false
		}
	}
	return true
}
