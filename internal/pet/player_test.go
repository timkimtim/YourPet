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
	for _, id := range []string{FullscreenHideID, FullscreenShowID} {
		clip := pkg.Clips[id]
		if clip == nil || len(clip.Frames) < 5 || clip.Length > time.Second {
			t.Fatalf("missing or invalid built-in transition %q", id)
		}
	}
	for _, id := range []string{"transition:sleep:sit", "transition:stand:sit"} {
		if clip := pkg.Clips[id]; clip == nil || len(clip.Frames) < 3 {
			t.Fatalf("missing posture transition %q", id)
		}
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
	if pose.Move == 0 && player.transition.clip != nil {
		input.Now = input.Now.Add(player.transition.clip.Length)
		pose = player.Tick(input)
	}
	if pose.Move <= 0 || pose.Cell != pkg.Clips[pkg.Walk.Right].Frames[0].Cell {
		t.Fatalf("walk should go right when left edge is blocked: %+v", pose)
	}
	input.Now = input.Now.Add(40 * time.Millisecond)
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
	player.Tick(input)
	input.Now = input.Now.Add(attentionLinger)
	if pose := player.Tick(input); pose.Cell != initialIdle {
		t.Fatalf("gaze did not return to resting pose: %+v", pose)
	}
}

func TestWakeFollowAndReturnToTheSameSleep(t *testing.T) {
	pkg := fixture(t)
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 7)
	player.startIdle(2, now)
	player.posture = player.idleID()
	bridge := pkg.Clips["transition:sleep:sit"]
	sleep := pkg.Clips["sleep"]
	input := Input{Now: now, CursorX: 500, LookRadius: 160, AutoWalk: true}
	if pose := player.Tick(input); pose.clip != sleep {
		t.Fatalf("expected to begin sleeping: %+v", pose)
	}
	input.Now, input.CursorX = now.Add(time.Second), 60
	if pose := player.Tick(input); pose.clip != bridge || pose.Cell != bridge.Frames[0].Cell {
		t.Fatalf("cursor skipped the wake-up sequence: %+v", pose)
	}
	input.Now = input.Now.Add(bridge.Length)
	if pose := player.Tick(input); pose.Cell != pkg.Look.Directions[4] {
		t.Fatalf("awake pet did not follow the cursor: %+v", pose)
	}
	input.Now = input.Now.Add(time.Minute)
	player.Tick(input)
	input.CursorX = 500
	player.Tick(input)
	input.Now = input.Now.Add(attentionLinger / 2)
	if pose := player.Tick(input); pose.clip != nil {
		t.Fatalf("a short departure from the radius should not change posture: %+v", pose)
	}
	input.Now = input.Now.Add(attentionLinger / 2)
	if pose := player.Tick(input); pose.clip != bridge || !player.transition.reversed {
		t.Fatalf("cursor leaving did not start settling back to sleep: %+v", pose)
	}
	input.Now = input.Now.Add(bridge.Length)
	if pose := player.Tick(input); pose.clip != sleep || !player.idleUntil.After(input.Now) {
		t.Fatalf("long attention should restore sleep with a fresh rest interval: %+v", pose)
	}
}

func TestJumpWakesThePetAndIgnoresASecondClickInFlight(t *testing.T) {
	pkg := fixture(t)
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 8)
	player.startIdle(2, now)
	player.posture = player.idleID()
	bridge := pkg.Clips["transition:sleep:sit"]
	jump := pkg.Clips[pkg.Jump]
	player.Jump(now)
	if pose := player.Tick(Input{Now: now}); pose.clip != bridge {
		t.Fatalf("sleeping pet jumped without waking: %+v", pose)
	}
	now = now.Add(bridge.Length)
	if pose := player.Tick(Input{Now: now}); pose.Cell != jump.Frames[0].Cell {
		t.Fatalf("queued jump was skipped: %+v", pose)
	}
	now = now.Add(time.Duration(jump.Frames[0].MS) * time.Millisecond)
	player.Jump(now)
	if pose := player.Tick(Input{Now: now}); pose.Cell != jump.Frames[1].Cell {
		t.Fatalf("second click restarted the jump: %+v", pose)
	}
}

func TestPostureTransitionReversesFromTheCurrentFrame(t *testing.T) {
	pkg := fixture(t)
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 8)
	player.movePosture("sleep", now)
	now = now.Add(175 * time.Millisecond)
	before, _ := player.movePosture("sleep", now)
	after, moving := player.movePosture("sit", now)
	if !moving || before.Cell != after.Cell || player.transition.reversed {
		t.Fatalf("reversing changed the current posture frame: before=%+v after=%+v", before, after)
	}
}

func TestOlderPackageStillBlendsPostures(t *testing.T) {
	pkg := fixture(t)
	delete(pkg.Clips, "transition:sleep:sit")
	now := time.Unix(100, 0)
	player := NewPlayer(pkg, now, 8)
	player.startIdle(2, now)
	player.posture = player.idleID()
	initial := player.Tick(Input{Now: now})
	input := Input{Now: now.Add(10 * time.Millisecond), CursorX: 80, LookRadius: 160}
	if pose := player.Tick(input); pose.Cell == initial.Cell || pose.FadeFrom != initial.Cell || pose.FadeAlpha != 255 {
		t.Fatalf("expected a fade when no posture clip is present: %+v", pose)
	}
	input.Now = input.Now.Add(poseFade / 2)
	if pose := player.Tick(input); pose.FadeAlpha < 120 || pose.FadeAlpha > 135 {
		t.Fatalf("fade did not interpolate: %+v", pose)
	}
	input.Now = input.Now.Add(poseFade / 2)
	if pose := player.Tick(input); pose.FadeAlpha != 0 {
		t.Fatalf("fade did not finish: %+v", pose)
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
