package pet

import "time"

type postureTransition struct {
	clip     *Animation
	reversed bool
	from, to string
	start    time.Time
}

func (p *Player) neutral() string {
	if p.Pet.Look != nil {
		return p.Pet.Look.Neutral
	}
	return p.Pet.Idle[0].Animation
}

func (p *Player) bridgeBetween(from, to string) (*Animation, bool) {
	if clip := p.Pet.Clips["transition:"+from+":"+to]; clip != nil {
		return clip, false
	}
	return p.Pet.Clips["transition:"+to+":"+from], true
}

// Reversing toward the starting pose keeps the current frame of the weight shift.
func (p *Player) movePosture(target string, now time.Time) (Pose, bool) {
	if current := p.transition; current.clip != nil {
		elapsed := now.Sub(current.start)
		if elapsed < current.clip.Length {
			if target == current.from {
				elapsed = current.clip.Length - elapsed - time.Nanosecond
				current.from, current.to = current.to, current.from
				current.reversed, current.start = !current.reversed, now.Add(-elapsed)
				p.transition = current
			}
			if current.reversed {
				elapsed = current.clip.Length - elapsed - time.Nanosecond
			}
			return clipPose(current.clip, elapsed), true
		}
		p.posture = current.to
		p.transition = postureTransition{}
		if p.posture == p.idleID() && !p.restHeld {
			p.startIdle(p.idleIndex, now)
		}
	}
	if p.posture == target {
		return Pose{}, false
	}

	next := target
	clip, reversed := p.bridgeBetween(p.posture, target)
	neutral := p.neutral()
	if clip == nil && p.posture != neutral && target != neutral {
		first, back := p.bridgeBetween(p.posture, neutral)
		second, _ := p.bridgeBetween(neutral, target)
		if first != nil && second != nil {
			next, clip, reversed = neutral, first, back
		}
	}
	if clip == nil {
		p.posture = target
		return Pose{}, false
	}
	p.transition = postureTransition{clip: clip, reversed: reversed, from: p.posture, to: next, start: now}
	var elapsed time.Duration
	if reversed {
		elapsed = clip.Length - time.Nanosecond
	}
	return clipPose(clip, elapsed), true
}
