package pet

import (
	"math"
	"math/rand"
	"time"
)

const (
	attentionLinger = 260 * time.Millisecond
	poseFade        = 96 * time.Millisecond
)

type Input struct {
	Now                       time.Time
	CursorX, CursorY          float64
	LookRadius                float64
	AutoWalk                  bool
	Dragging                  bool
	DragDirection             int
	CanWalkLeft, CanWalkRight bool
}

type Pose struct {
	Cell      Cell
	FadeFrom  Cell
	FadeAlpha uint8
	Move      float64
	clip      *Animation
}

type Player struct {
	Pet                            *Package
	random                         *rand.Rand
	idleIndex                      int
	idleStart, idleUntil           time.Time
	posture                        string
	transition                     postureTransition
	restHeld                       bool
	nextWalk, walkStart, walkUntil time.Time
	walkDirection                  int
	lastTick                       time.Time
	dragStart                      time.Time
	action                         *Animation
	actionStart                    time.Time
	attentive                      bool
	awaySince                      time.Time
	tracking                       bool
	angle                          float64
	lastPose                       Pose
	rendered                       bool
	fadeStart                      time.Time
	fadeFrom                       Cell
}

func NewPlayer(p *Package, now time.Time, seed int64) *Player {
	player := &Player{Pet: p, random: rand.New(rand.NewSource(seed)), lastTick: now, posture: p.Idle[0].Animation}
	player.startIdle(0, now)
	player.scheduleWalk(now)
	return player
}

func clipPose(clip *Animation, elapsed time.Duration) Pose {
	return Pose{Cell: clip.At(elapsed), clip: clip}
}

func (p *Player) idleID() string {
	return p.Pet.Idle[p.idleIndex].Animation
}

func (p *Player) startIdle(index int, now time.Time) {
	state := p.Pet.Idle[index]
	p.idleIndex, p.idleStart = index, now
	p.idleUntil = now.Add(time.Duration(state.MinMS+p.random.Intn(state.MaxMS-state.MinMS+1)) * time.Millisecond)
}

func (p *Player) NextIdle(now time.Time) {
	p.changeIdle(now)
	p.scheduleWalk(now)
}

func (p *Player) changeIdle(now time.Time) {
	size := len(p.Pet.Idle)
	index := p.idleIndex
	if size > 1 {
		index = (index + 1 + p.random.Intn(size-1)) % size
	}
	p.startIdle(index, now)
}

func (p *Player) scheduleWalk(now time.Time) {
	p.walkDirection = 0
	p.nextWalk = now.Add(time.Duration(14+p.random.Intn(21)) * time.Second)
}

func (p *Player) Play(id string, now time.Time) {
	if clip := p.Pet.Clips[id]; clip != nil {
		p.action, p.actionStart = clip, time.Time{}
		p.scheduleWalk(now)
	}
}

func (p *Player) Jump(now time.Time) {
	if p.Pet.Jump != "" && p.action != p.Pet.Clips[p.Pet.Jump] {
		p.Play(p.Pet.Jump, now)
	}
}

func (p *Player) Tick(input Input) Pose {
	pose := p.tick(input)
	if p.rendered && p.lastPose.clip != pose.clip {
		p.fadeStart = time.Time{}
		if p.lastPose.Cell != pose.Cell {
			p.fadeStart, p.fadeFrom = input.Now, p.lastPose.Cell
		}
	}
	if !p.fadeStart.IsZero() {
		elapsed := input.Now.Sub(p.fadeStart)
		if elapsed >= poseFade {
			p.fadeStart = time.Time{}
		} else {
			ratio := float64(elapsed) / float64(poseFade)
			pose.FadeFrom = p.fadeFrom
			pose.FadeAlpha = uint8(math.Round(255 * (1 - ratio*ratio*(3-2*ratio))))
		}
	}
	p.lastPose, p.rendered = pose, true
	return pose
}

func (p *Player) tick(input Input) Pose {
	now := input.Now
	delta := math.Max(0, math.Min(0.08, now.Sub(p.lastTick).Seconds()))
	p.lastTick = now
	if input.Dragging {
		p.tracking = false
		p.restHeld = true
		if p.dragStart.IsZero() {
			p.dragStart = now
			p.scheduleWalk(now)
		}
		if input.DragDirection != 0 && p.Pet.Walk != nil {
			p.posture, p.transition = p.neutral(), postureTransition{}
			return clipPose(p.walkClip(input.DragDirection), now.Sub(p.dragStart))
		}
		if p.rendered {
			return Pose{Cell: p.lastPose.Cell, clip: p.lastPose.clip}
		}
		return p.idlePose(now)
	}
	p.dragStart = time.Time{}

	p.updateAttention(input)
	if p.action != nil {
		p.restHeld, p.tracking = true, false
		if p.actionStart.IsZero() {
			if pose, moving := p.movePosture(p.neutral(), now); moving {
				return pose
			}
			p.actionStart = now
		}
		if elapsed := now.Sub(p.actionStart); elapsed < p.action.Length {
			return clipPose(p.action, elapsed)
		}
		p.action = nil
	}
	if p.attentive {
		p.restHeld = true
		if p.walkDirection != 0 {
			p.scheduleWalk(now)
		}
		if pose, moving := p.movePosture(p.neutral(), now); moving {
			return pose
		}
		return p.lookPose(input, delta)
	}
	if p.restHeld {
		p.restHeld = false
		// Resume the same rest with a fresh interval, even after a long hover.
		p.startIdle(p.idleIndex, now)
		p.scheduleWalk(now)
	}

	if !input.AutoWalk && p.walkDirection != 0 {
		p.scheduleWalk(now)
	}
	if input.AutoWalk && p.Pet.Walk != nil {
		if p.walkDirection != 0 && (!now.Before(p.walkUntil) ||
			(p.walkDirection < 0 && !input.CanWalkLeft) || (p.walkDirection > 0 && !input.CanWalkRight)) {
			p.scheduleWalk(now)
			p.startIdle(0, now)
		}
		if p.walkDirection == 0 && !now.Before(p.nextWalk) && (input.CanWalkLeft || input.CanWalkRight) {
			if pose, moving := p.movePosture(p.neutral(), now); moving {
				return pose
			}
			p.beginWalk(input)
		}
		if p.walkDirection != 0 {
			pose := clipPose(p.walkClip(p.walkDirection), now.Sub(p.walkStart))
			pose.Move = float64(p.walkDirection) * 65 * delta
			return pose
		}
	}
	if pose, moving := p.movePosture(p.idleID(), now); moving {
		return pose
	}
	if !now.Before(p.idleUntil) {
		p.changeIdle(now)
		if pose, moving := p.movePosture(p.idleID(), now); moving {
			return pose
		}
	}
	return p.idlePose(now)
}

func (p *Player) idlePose(now time.Time) Pose {
	return clipPose(p.Pet.Clips[p.idleID()], now.Sub(p.idleStart))
}

func (p *Player) beginWalk(input Input) {
	p.scheduleWalk(input.Now)
	if input.CanWalkLeft && (p.random.Intn(2) == 0 || !input.CanWalkRight) {
		p.walkDirection = -1
	} else if input.CanWalkRight {
		p.walkDirection = 1
	}
	p.walkStart = input.Now
	p.walkUntil = input.Now.Add(time.Duration(2200+p.random.Intn(3401)) * time.Millisecond)
}

func (p *Player) walkClip(direction int) *Animation {
	if direction < 0 {
		return p.Pet.Clips[p.Pet.Walk.Left]
	}
	return p.Pet.Clips[p.Pet.Walk.Right]
}

func (p *Player) updateAttention(input Input) {
	radius := input.LookRadius
	if p.attentive {
		radius += 14
	}
	switch {
	case p.Pet.Look == nil || input.LookRadius <= 0:
		p.attentive = false
	case math.Hypot(input.CursorX, input.CursorY) <= radius:
		p.attentive, p.awaySince = true, time.Time{}
	case p.attentive && p.awaySince.IsZero():
		p.awaySince = input.Now
	case p.attentive && input.Now.Sub(p.awaySince) >= attentionLinger:
		p.attentive = false
	}
	if !p.attentive {
		p.tracking = false
		p.awaySince = time.Time{}
	}
}

func (p *Player) lookPose(input Input, delta float64) Pose {
	look := p.Pet.Look
	neutral := p.Pet.Clips[look.Neutral].Frames[0].Cell
	if !p.awaySince.IsZero() {
		if p.rendered && p.lastPose.clip == nil {
			return Pose{Cell: p.lastPose.Cell}
		}
		return Pose{Cell: neutral}
	}
	if math.Hypot(input.CursorX, input.CursorY) < 10 {
		p.tracking = false
		return Pose{Cell: neutral}
	}
	target := math.Mod(math.Atan2(input.CursorX, -input.CursorY)*180/math.Pi+360, 360)
	if !p.tracking {
		p.angle = target
		p.tracking = true
	}
	difference := math.Mod(target-p.angle+540, 360) - 180
	step := 780 * delta
	p.angle = math.Mod(p.angle+math.Max(-step, math.Min(step, difference))+360, 360)
	if math.Abs(difference) < 0.75 {
		p.angle = target
	}
	n := len(look.Directions)
	index := int(math.Round(p.angle*float64(n)/360)) % n
	return Pose{Cell: look.Directions[index]}
}
