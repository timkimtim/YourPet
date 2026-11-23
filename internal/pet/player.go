package pet

import (
	"math"
	"math/rand"
	"time"
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
	Cell Cell
	Move float64
}

type Player struct {
	Pet                            *Package
	random                         *rand.Rand
	idleIndex                      int
	idleStart, idleUntil           time.Time
	nextWalk, walkStart, walkUntil time.Time
	walkDirection                  int
	lastTick                       time.Time
	dragStart                      time.Time
	action                         *Animation
	actionStart                    time.Time
	tracking                       bool
	angle                          float64
}

func NewPlayer(p *Package, now time.Time, seed int64) *Player {
	player := &Player{Pet: p, random: rand.New(rand.NewSource(seed)), lastTick: now}
	player.startIdle(0, now)
	player.scheduleWalk(now)
	return player
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
		p.action, p.actionStart = clip, now
		p.scheduleWalk(now)
	}
}

func (p *Player) Tick(input Input) Pose {
	now := input.Now
	delta := math.Max(0, math.Min(0.08, now.Sub(p.lastTick).Seconds()))
	p.lastTick = now
	if input.Dragging {
		p.tracking = false
		if p.dragStart.IsZero() {
			p.dragStart = now
			p.scheduleWalk(now)
		}
		if input.DragDirection != 0 && p.Pet.Walk != nil {
			return Pose{Cell: p.walkClip(input.DragDirection).At(now.Sub(p.dragStart))}
		}
		return p.idlePose(now)
	}
	p.dragStart = time.Time{}
	if p.action != nil {
		elapsed := now.Sub(p.actionStart)
		if elapsed < p.action.Length {
			p.tracking = false
			return Pose{Cell: p.action.At(elapsed)}
		}
		p.action = nil
		p.startIdle(p.idleIndex, now)
	}
	if gaze, ok := p.lookPose(input, delta); ok {
		if p.walkDirection != 0 {
			p.scheduleWalk(now)
		}
		return gaze
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
		if p.walkDirection == 0 && !now.Before(p.nextWalk) {
			p.beginWalk(input)
		}
		if p.walkDirection != 0 {
			return Pose{Cell: p.walkClip(p.walkDirection).At(now.Sub(p.walkStart)), Move: float64(p.walkDirection) * 65 * delta}
		}
	}
	if !now.Before(p.idleUntil) {
		p.changeIdle(now)
	}
	return p.idlePose(now)
}

func (p *Player) idlePose(now time.Time) Pose {
	clip := p.Pet.Clips[p.Pet.Idle[p.idleIndex].Animation]
	return Pose{Cell: clip.At(now.Sub(p.idleStart))}
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

func (p *Player) lookPose(input Input, delta float64) (Pose, bool) {
	look := p.Pet.Look
	radius := input.LookRadius
	if p.tracking {
		radius += 14
	}
	distance := math.Hypot(input.CursorX, input.CursorY)
	if look == nil || input.LookRadius <= 0 || distance > radius {
		if p.tracking {
			p.tracking = false
			p.startIdle(p.idleIndex, input.Now)
			p.scheduleWalk(input.Now)
		}
		return Pose{}, false
	}
	if distance < 10 {
		p.tracking = false
		return Pose{Cell: p.Pet.Clips[look.Neutral].Frames[0].Cell}, true
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
	return Pose{Cell: look.Directions[index]}, true
}
