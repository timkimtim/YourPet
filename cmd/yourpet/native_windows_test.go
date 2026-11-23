package main

import (
	"runtime"
	"testing"
	"time"

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
