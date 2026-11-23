package main

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/timkimtim/YourPet/internal/pet"
)

func TestNativeOverlay(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	setDPIContext.Call(^uintptr(3))
	a := &app{settings: settings{Scale: 100, Radius: 260, Topmost: true}, hidden: true}
	instance = a
	pkg, err := a.defaultPackage()
	if err != nil || pkg == nil {
		t.Fatalf("embedded package: %v", err)
	}
	if err = a.makeWindow(); err != nil {
		t.Fatal(err)
	}
	defer destroyWindow.Call(a.window)
	defer a.surface.close()
	a.setPet(pkg)
	cell := pkg.Clips[pkg.Idle[0].Animation].Frames[0].Cell
	w, h := a.size()
	if err = a.paint(cell, w, h); err != nil {
		t.Fatalf("present the first translucent frame: %v", err)
	}
	a.settings.ClickThrough = true
	a.refreshStyle()
	style, _, _ := getWindowLong.Call(a.window, ^uintptr(19))
	if style&wsExTransparent == 0 {
		t.Fatal("click-through did not set the layered window style")
	}
	a.settings.ClickThrough = false
	a.refreshStyle()
	style, _, _ = getWindowLong.Call(a.window, ^uintptr(19))
	if style&wsExTransparent != 0 {
		t.Fatal("click-through could not be turned off")
	}
	if a.lastCell != cell {
		t.Fatal("unexpected frame was displayed")
	}
	a.hidden = false
	a.fullscreenCheckAt = time.Now().Add(time.Minute)
	a.tick()
	if !a.windowShown {
		t.Fatal("overlay did not appear")
	}
	a.dragDown = true
	a.fullscreen = true
	if a.handleFullscreen(time.Now(), w, h) || !a.windowShown {
		t.Fatal("a fullscreen window interrupted an ongoing drag")
	}
	a.endDrag()
	if !a.fullscreenCheckAt.IsZero() {
		t.Fatal("dropping the pet did not invalidate the previous monitor check")
	}
	a.fullscreenCheckAt = time.Now().Add(time.Minute)
	a.fullscreen = true
	a.tick()
	if a.windowShown {
		t.Fatal("fullscreen did not hide the overlay")
	}
	a.fullscreen = false
	a.tick()
	if !a.windowShown {
		t.Fatal("overlay did not return after leaving fullscreen")
	}

	a.settings.AnimateFullscreen = true
	a.fullscreen = true
	a.tick()
	if !a.windowShown || !a.portalActive || a.lastCell != pkg.Clips[pet.FullscreenHideID].Frames[0].Cell {
		t.Fatal("fullscreen did not draw the portal departure")
	}
	style, _, _ = getWindowLong.Call(a.window, ^uintptr(19))
	if style&wsExTransparent == 0 {
		t.Fatal("portal animation is intercepting clicks")
	}
	a.exclusiveFullscreen = true
	a.tick()
	if a.windowShown || a.portalActive {
		t.Fatal("exclusive fullscreen did not hide the overlay immediately")
	}
	a.fullscreen, a.exclusiveFullscreen = false, false
	a.tick()
	if !a.windowShown || !a.portalActive || a.lastCell != pkg.Clips[pet.FullscreenShowID].Frames[0].Cell {
		t.Fatal("return did not draw the portal arrival")
	}
}

func TestFullscreenGeometry(t *testing.T) {
	monitor := rectangle{Left: -1920, Top: 0, Right: 0, Bottom: 1080}
	if !occupiesMonitor(monitor, monitor, false) {
		t.Fatal("borderless video should count as fullscreen")
	}
	if occupiesMonitor(monitor, monitor, true) {
		t.Fatal("captioned maximized window should remain visible even with an auto-hidden taskbar")
	}
	if occupiesMonitor(rectangle{-1920, 0, 0, 1032}, monitor, false) {
		t.Fatal("window that leaves a taskbar gap should not count as fullscreen")
	}
	if occupiesMonitor(rectangle{0, 0, 1920, 1080}, monitor, false) {
		t.Fatal("fullscreen on another monitor should not hide this pet")
	}
}

func TestFullscreenTracksPetMonitor(t *testing.T) {
	primary := rectangle{0, 0, 1920, 1080}
	secondary := rectangle{-1920, 0, 0, 1080}
	video := displayWindow{handle: 11, monitor: 1, bounds: primary}
	secondScreenApp := displayWindow{handle: 12, monitor: 2, bounds: secondary, captionedMaximized: true}
	scan := fullscreenScan{monitor: 1, bounds: primary}
	if scan.consider(secondScreenApp) || !scan.consider(video) || scan.coveringWindow != video.handle {
		t.Fatal("an active app on the second monitor hid the fullscreen video from the scan")
	}

	scan = fullscreenScan{monitor: 2, bounds: secondary}
	if scan.consider(video) || !scan.consider(secondScreenApp) || scan.coveringWindow != 0 {
		t.Fatal("fullscreen on the first monitor affected a pet moved to the second monitor")
	}

	scan = fullscreenScan{monitor: 2, bounds: secondary}
	fullscreenOnSecondary := displayWindow{handle: 13, monitor: 2, bounds: secondary}
	if scan.consider(video) || !scan.consider(fullscreenOnSecondary) || scan.coveringWindow != fullscreenOnSecondary.handle {
		t.Fatal("the destination monitor's own fullscreen window was missed")
	}
}

func TestNativeFullscreenWindowStack(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	setDPIContext.Call(^uintptr(3))
	pos, _ := cursorPosition()
	monitor := displayInfo(monitorAt(pos)).Monitor
	if monitor.Right <= monitor.Left || monitor.Bottom <= monitor.Top {
		t.Fatal("no monitor bounds for the native fullscreen test")
	}
	center := point{(monitor.Left + monitor.Right) / 2, (monitor.Top + monitor.Bottom) / 2}
	old := instance
	a := &app{}
	instance = a
	defer func() { instance = old }()
	var windows []uintptr
	defer func() {
		for _, hwnd := range windows {
			destroyWindow.Call(hwnd)
		}
	}()

	show := func(bounds rectangle, style uintptr) uintptr {
		t.Helper()
		name := utf16("Static")
		hwnd, _, err := createWindow.Call(style, uintptr(unsafe.Pointer(name)), 0, 0x80000000,
			uintptr(bounds.Left), uintptr(bounds.Top), uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top),
			0, 0, 0, 0)
		if hwnd == 0 {
			t.Fatal(err)
		}
		windows = append(windows, hwnd)
		ok, _, err := setWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10|0x40)
		if ok == 0 {
			t.Fatal(err)
		}
		return hwnd
	}

	video := show(monitor, 0)
	if got := a.fullscreenWindowAt(center); got != video {
		t.Fatalf("fullscreen window was missed: got %v, want %v", got, video)
	}
	small := rectangle{center.X - 50, center.Y - 50, center.X + 50, center.Y + 50}
	show(small, wsExToolWindow)
	if got := a.fullscreenWindowAt(center); got != video {
		t.Fatal("tool window above the video interrupted fullscreen detection")
	}
	top := show(small, 0)
	if got := a.fullscreenWindowAt(center); got != 0 {
		t.Fatal("fullscreen video behind an ordinary window still hid the pet")
	}
	showWindow.Call(top, 0)
	if got := a.fullscreenWindowAt(center); got != video {
		t.Fatal("visible fullscreen video was not detected again")
	}
}
