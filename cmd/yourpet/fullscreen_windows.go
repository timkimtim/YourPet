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
	dwmWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
	notificationState  = shell32.NewProc("SHQueryUserNotificationState")
)

func (a *app) handleFullscreen(now time.Time, width, height int) bool {
	if !now.Before(a.fullscreenCheckAt) {
		center := point{int32(a.x) + int32(width/2), int32(a.y) + int32(height/2)}
		a.fullscreen = a.settings.HideFullscreen && foregroundFillsMonitor(center, a.window)
		a.exclusiveFullscreen = a.fullscreen && exclusiveGameActive()
		a.fullscreenCheckAt = now.Add(160 * time.Millisecond)
	}
	var leave, arrive *pet.Animation
	if a.settings.AnimateFullscreen {
		leave = a.player.Pet.Clips[pet.FullscreenHideID]
		arrive = a.player.Pet.Clips[pet.FullscreenShowID]
	}
	canLeave := a.windowShown && a.settings.Topmost && !a.exclusiveFullscreen && !a.dragDown
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

func foregroundFillsMonitor(at point, ownWindow uintptr) bool {
	foreground, _, _ := foregroundWindow.Call()
	shell, _, _ := shellWindow.Call()
	if foreground == 0 || foreground == ownWindow || foreground == shell {
		return false
	}
	if minimized, _, _ := windowMinimized.Call(foreground); minimized != 0 {
		return false
	}
	var className [64]uint16
	windowClassName.Call(foreground, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	switch syscall.UTF16ToString(className[:]) {
	case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
		return false
	}
	var bounds rectangle
	// DWM bounds exclude the invisible resize border around an ordinary window.
	code, _, _ := dwmWindowAttribute.Call(foreground, 9, uintptr(unsafe.Pointer(&bounds)), unsafe.Sizeof(bounds))
	if int32(code) < 0 {
		ok, _, _ := windowRect.Call(foreground, uintptr(unsafe.Pointer(&bounds)))
		if ok == 0 {
			return false
		}
	}
	style, _, _ := getWindowLong.Call(foreground, ^uintptr(15)) // GWL_STYLE
	maximized, _, _ := windowMaximized.Call(foreground)
	captioned := style&0x00c00000 == 0x00c00000 // WS_CAPTION
	return occupiesMonitor(bounds, displayInfo(at).Monitor, maximized != 0 && captioned)
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
