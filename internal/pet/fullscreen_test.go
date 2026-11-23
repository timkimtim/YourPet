package pet

import (
	"testing"
	"time"
)

func TestFullscreenMotion(t *testing.T) {
	now := time.Unix(0, 0)
	leave := &Animation{Frames: []Frame{{Cell: Cell{Row: 1}, MS: 80}, {Cell: Cell{Row: 1, Col: 1}, MS: 120}}, Length: 200 * time.Millisecond}
	arrive := &Animation{Frames: []Frame{{Cell: Cell{Row: 2}, MS: 120}}, Length: 120 * time.Millisecond}
	var motion FullscreenMotion
	if got := motion.Step(now, false, true, leave, arrive); got.Hidden || got.Animating {
		t.Fatalf("initial state must be ordinary idle: %+v", got)
	}
	if got := motion.Step(now, true, true, leave, arrive); !got.Animating || got.Cell.Row != 1 {
		t.Fatalf("fullscreen did not start departure: %+v", got)
	}
	if got := motion.Step(now.Add(80*time.Millisecond), true, true, leave, arrive); !got.Animating || got.Cell.Col != 1 {
		t.Fatalf("departure did not advance: %+v", got)
	}
	if got := motion.Step(now.Add(200*time.Millisecond), true, true, leave, arrive); !got.Hidden {
		t.Fatalf("departure did not end in hiding: %+v", got)
	}
	if got := motion.Step(now.Add(time.Second), true, false, leave, arrive); !got.Hidden {
		t.Fatalf("fullscreen did not stay hidden: %+v", got)
	}
	if got := motion.Step(now.Add(2*time.Second), false, false, leave, arrive); !got.Animating || got.Cell.Row != 2 {
		t.Fatalf("leaving fullscreen did not start return: %+v", got)
	}
	if got := motion.Step(now.Add(3*time.Second), false, true, leave, arrive); got.Hidden || got.Animating {
		t.Fatalf("return did not restore ordinary idle: %+v", got)
	}
}

func TestFullscreenSkipsDepartureWhenOverlayCannotBeSeen(t *testing.T) {
	now := time.Unix(0, 0)
	clip := &Animation{Frames: []Frame{{Cell: Cell{Col: 2}, MS: 80}}, Length: 80 * time.Millisecond}
	var motion FullscreenMotion
	if got := motion.Step(now, true, false, clip, clip); !got.Hidden {
		t.Fatalf("exclusive fullscreen must hide immediately: %+v", got)
	}
	if got := motion.Step(now.Add(time.Second), false, false, clip, clip); !got.Animating {
		t.Fatalf("exclusive fullscreen must still animate the return: %+v", got)
	}
	if got := motion.Step(now.Add(time.Second), true, false, clip, clip); !got.Hidden {
		t.Fatalf("reentering fullscreen must interrupt return: %+v", got)
	}
}

func TestFullscreenWithoutPortalClips(t *testing.T) {
	now := time.Unix(0, 0)
	var motion FullscreenMotion
	if got := motion.Step(now, true, true, nil, nil); !got.Hidden {
		t.Fatalf("legacy package did not hide: %+v", got)
	}
	if got := motion.Step(now.Add(time.Second), false, false, nil, nil); got.Hidden || got.Animating {
		t.Fatalf("legacy package did not restore ordinary idle: %+v", got)
	}
}

func TestFullscreenTransitionDurationIsBounded(t *testing.T) {
	now := time.Unix(0, 0)
	clip := &Animation{Frames: []Frame{{MS: 5000}}, Length: 5 * time.Second}
	var motion FullscreenMotion
	motion.Step(now, true, true, clip, nil)
	if got := motion.Step(now.Add(time.Second), true, true, clip, nil); !got.Hidden {
		t.Fatalf("departure kept covering fullscreen for too long: %+v", got)
	}
}
