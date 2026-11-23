package main

import (
	"runtime"
	"testing"
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
}
