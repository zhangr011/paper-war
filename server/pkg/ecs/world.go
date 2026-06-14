package ecs

import (
	"fmt"
	"sync"
)

type poolKey string

type World struct {
	em      *EntityManager
	sched   *Scheduler
	poolMap map[poolKey]interface{}
	poolMu  sync.RWMutex
}

func NewWorld(em *EntityManager) *World {
	return &World{
		em:      em,
		sched:   NewScheduler(),
		poolMap: make(map[poolKey]interface{}),
	}
}

func (w *World) Entities() *EntityManager { return w.em }

func (w *World) AddSystem(sys System) {
	w.sched.AddSystem(sys)
}

func (w *World) SystemByName(name string) System {
	return w.sched.SystemByName(name)
}

func (w *World) RegisterPool(sample interface{}, pool interface{}) {
	w.poolMu.Lock()
	defer w.poolMu.Unlock()
	w.poolMap[poolKey(fmt.Sprintf("%T", sample))] = pool
}

func (w *World) Pool(sample interface{}) interface{} {
	w.poolMu.RLock()
	defer w.poolMu.RUnlock()
	return w.poolMap[poolKey(fmt.Sprintf("%T", sample))]
}

func (w *World) Init() {
	w.sched.Init(w)
}

func (w *World) Tick(tick uint32) {
	w.sched.Tick(w, tick)
}