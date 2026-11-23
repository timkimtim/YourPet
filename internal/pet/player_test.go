package pet

import (
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T) *Package {
	t.Helper()
	name := os.Getenv("YOURPET_TEST_PACKAGE")
	if name == "" {
		name = "../../cmd/yourpet/default.pet"
	}
	pkg, _, err := ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestBundledPackage(t *testing.T) {
	pkg := fixture(t)
	if len(pkg.Idle) != 3 || pkg.Look == nil ||
		len(pkg.Look.Directions) != 16 || pkg.Walk == nil || pkg.Jump == "" {
		t.Fatal("built-in package has missing features")
	}
}

func TestAmbientSwitchAndWalk(t *testing.T) {
	pkg := fixture(t)
	start := time.Unix(100, 0)
	player := NewPlayer(pkg, start, 42)
	initial := player.idleIndex
	player.Tick(Input{Now: player.idleUntil})
	if player.idleIndex == initial {
		t.Fatal("idle pose did not change")
	}
	now := player.nextWalk
	input := Input{Now: now, AutoWalk: true, CanWalkRight: true}
	pose := player.Tick(input)
	if pose.Move <= 0 || pose.Cell != pkg.Clips[pkg.Walk.Right].Frames[0].Cell {
		t.Fatalf("walk should go right when left edge is blocked: %+v", pose)
	}
	input.Now = now.Add(40 * time.Millisecond)
	input.CanWalkRight = false
	if pose := player.Tick(input); pose.Move != 0 {
		t.Fatalf("walk continued beyond the edge: %+v", pose)
	}
}

func TestLookRadiusAndHeadTurn(t *testing.T) {
	pkg := fixture(t)
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 5)
	input := Input{Now: now, CursorX: 151, LookRadius: 150}
	initialIdle := pkg.Clips[pkg.Idle[0].Animation].Frames[0].Cell
	if pose := player.Tick(input); pose.Cell != initialIdle {
		t.Fatalf("outside radius: got %+v", pose)
	}
	input.CursorX = 100
	right := len(pkg.Look.Directions) / 4
	left := right * 3
	if pose := player.Tick(input); pose.Cell != pkg.Look.Directions[right] {
		t.Fatalf("rightward gaze: got %+v", pose)
	}
	input.Now = now.Add(48 * time.Millisecond)
	input.CursorX = -100
	intermediate := player.Tick(input).Cell
	if intermediate == pkg.Look.Directions[right] || intermediate == pkg.Look.Directions[left] {
		t.Fatalf("head skipped the intermediate turn: %+v", intermediate)
	}
	for range 12 {
		input.Now = input.Now.Add(32 * time.Millisecond)
		player.Tick(input)
	}
	if pose := player.Tick(input); pose.Cell != pkg.Look.Directions[left] {
		t.Fatalf("leftward gaze: got %+v", pose)
	}
	input.Now = input.Now.Add(50 * time.Millisecond)
	input.CursorX = -200
	if pose := player.Tick(input); pose.Cell != initialIdle {
		t.Fatalf("gaze did not return to resting pose: %+v", pose)
	}
}

func TestJumpAndDragOverrideGaze(t *testing.T) {
	pkg := fixture(t)
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 4)
	player.Play(pkg.Jump, now)
	input := Input{Now: now, CursorY: -100, LookRadius: 260}
	if pose := player.Tick(input); pose.Cell != pkg.Clips[pkg.Jump].Frames[0].Cell {
		t.Fatalf("expected jump instead of gaze: %+v", pose)
	}
	input.Now = now.Add(pkg.Clips[pkg.Jump].Length)
	input.DragDirection = -1
	input.Dragging = true
	if pose := player.Tick(input); pose.Cell != pkg.Clips[pkg.Walk.Left].Frames[0].Cell || pose.Move != 0 {
		t.Fatalf("expected left animation without automatic movement while dragging: %+v", pose)
	}
}

func TestRejectInvalidPackage(t *testing.T) {
	if _, err := Decode([]byte("not a ZIP")); err == nil {
		t.Fatal("accepted invalid archive")
	}
	if _, err := Decode(make([]byte, MaxPackageBytes+1)); err == nil {
		t.Fatal("accepted oversized archive")
	}
}
