package ecs

import "sort"

type System interface {
	Name() string
	Priority() int
	Init(w *World)
	Tick(w *World, tick uint32)
}

type systemEntry struct {
	System
	initialized bool
}

type Scheduler struct {
	systems []systemEntry
}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

func (s *Scheduler) AddSystem(sys System) {
	s.systems = append(s.systems, systemEntry{System: sys})
	sort.Slice(s.systems, func(i, j int) bool {
		return s.systems[i].Priority() < s.systems[j].Priority()
	})
}

func (s *Scheduler) Init(w *World) {
	for i := range s.systems {
		if !s.systems[i].initialized {
			s.systems[i].Init(w)
			s.systems[i].initialized = true
		}
	}
}

func (s *Scheduler) Tick(w *World, tick uint32) {
	for i := range s.systems {
		s.systems[i].Tick(w, tick)
	}
}

func (s *Scheduler) SystemByName(name string) System {
	for i := range s.systems {
		if s.systems[i].Name() == name {
			return s.systems[i].System
		}
	}
	return nil
}