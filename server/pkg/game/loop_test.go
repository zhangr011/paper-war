package game

import (
	"sync/atomic"
	"testing"
	"time"
)

type counterSystem struct {
	count atomic.Int32
}

func (s *counterSystem) Name() string    { return "counter" }
func (s *counterSystem) Priority() int   { return 0 }
func (s *counterSystem) Init(_ interface{}) {}
func (s *counterSystem) Tick(_ interface{}, _ uint32) {
	s.count.Add(1)
}

func TestGameLoopRunAndStop(t *testing.T) {
	loop := NewLoop(15)

	cs := &counterSystem{}
	loop.AddSystem(cs)

	loop.Start()
	time.Sleep(200 * time.Millisecond)
	loop.Stop()

	c := cs.count.Load()
	if c < 2 || c > 5 {
		t.Errorf("after 200ms at 15Hz, tick count = %d, expected 2-5", c)
	}
}

func TestGameLoopStopIdempotent(t *testing.T) {
	loop := NewLoop(15)
	loop.Start()
	time.Sleep(50 * time.Millisecond)
	loop.Stop()
	loop.Stop()
}