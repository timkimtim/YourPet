package pet

import "time"

const (
	FullscreenHideID = "fullscreen-hide"
	FullscreenShowID = "fullscreen-show"
)

type fullscreenPhase uint8

const (
	onDesktop fullscreenPhase = iota
	leavingDesktop
	offDesktop
	returningToDesktop
)

// Display is the fullscreen policy's output for this tick. The ordinary
// animation player controls the window when both flags are false.
type Display struct {
	Hidden    bool
	Animating bool
	Cell      Cell
}

type FullscreenMotion struct {
	phase fullscreenPhase
	since time.Time
}

func (m *FullscreenMotion) Step(now time.Time, fullscreen, canLeaveVisibly bool, leave, arrive *Animation) Display {
	if fullscreen {
		if m.phase == onDesktop || m.phase == returningToDesktop {
			m.phase, m.since = leavingDesktop, now
		}
		if m.phase == leavingDesktop {
			if elapsed := now.Sub(m.since); canLeaveVisibly && leave != nil && elapsed < min(leave.Length, time.Second) {
				return Display{Animating: true, Cell: leave.At(elapsed)}
			}
			m.phase = offDesktop
		}
		return Display{Hidden: true}
	}

	if m.phase == offDesktop || m.phase == leavingDesktop {
		m.phase, m.since = returningToDesktop, now
	}
	if m.phase == returningToDesktop {
		if elapsed := now.Sub(m.since); arrive != nil && elapsed < min(arrive.Length, time.Second) {
			return Display{Animating: true, Cell: arrive.At(elapsed)}
		}
		m.phase = onDesktop
	}
	return Display{}
}
