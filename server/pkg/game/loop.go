package game

import (
	"sync"
	"time"
)

type System interface {
	Name() string
	Priority() int
	Init(world interface{})
	Tick(world interface{}, tick uint32)
}

type Loop struct {
	tickRate  int
	interval  time.Duration
	systems   []System
	tickCount uint32
	running   bool
	stopCh    chan struct{}
	mu        sync.Mutex
	world     interface{}
}

func NewLoop(tickRate int) *Loop {
	return &Loop{
		tickRate: tickRate,
		interval: time.Second / time.Duration(tickRate),
		systems:  make([]System, 0),
		stopCh:   make(chan struct{}),
	}
}

func (l *Loop) AddSystem(sys System) {
	l.systems = append(l.systems, sys)
}

func (l *Loop) SetWorld(w interface{}) {
	l.world = w
}

func (l *Loop) Start() {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return
	}
	l.running = true
	l.stopCh = make(chan struct{})
	l.mu.Unlock()

	for _, sys := range l.systems {
		sys.Init(l.world)
	}

	go l.run()
}

func (l *Loop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	l.running = false
	close(l.stopCh)
}

func (l *Loop) run() {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.tickCount++
			for _, sys := range l.systems {
				sys.Tick(l.world, l.tickCount)
			}
		}
	}
}

func (l *Loop) TickCount() uint32 {
	return l.tickCount
}

func (l *Loop) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}
