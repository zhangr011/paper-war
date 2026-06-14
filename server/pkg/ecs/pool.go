package ecs

import "sync"

// ComponentPool stores components of type T, indexed by Entity.
// Uses a sparse-set for O(1) add/get/remove and dense iteration.
type ComponentPool[T any] struct {
	mu     sync.RWMutex
	sparse []int32  // entity index → dense index (-1 = absent)
	dense  []uint32 // dense indices back into entity space
	data   []T      // packed component data
}

func NewComponentPool[T any]() *ComponentPool[T] {
	return &ComponentPool[T]{
		sparse: make([]int32, 0, 1024),
		dense:  make([]uint32, 0, 256),
		data:   make([]T, 0, 256),
	}
}

func (p *ComponentPool[T]) ensureSparse(idx uint32) {
	if int(idx) >= len(p.sparse) {
		newSparse := make([]int32, idx+1, idx*2)
		for i := range newSparse {
			newSparse[i] = -1
		}
		copy(newSparse, p.sparse)
		p.sparse = newSparse
	}
}

func (p *ComponentPool[T]) Add(e Entity, comp T) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := entityIdx(e)
	p.ensureSparse(idx)

	if p.sparse[idx] >= 0 {
		p.data[p.sparse[idx]] = comp
		return
	}

	di := int32(len(p.data))
	p.sparse[idx] = di
	p.dense = append(p.dense, idx)
	p.data = append(p.data, comp)
}

func (p *ComponentPool[T]) Get(e Entity) (T, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		var zero T
		return zero, false
	}
	di := p.sparse[idx]
	if di < 0 {
		var zero T
		return zero, false
	}
	return p.data[di], true
}

func (p *ComponentPool[T]) GetPtr(e Entity) (*T, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		return nil, false
	}
	di := p.sparse[idx]
	if di < 0 {
		return nil, false
	}
	return &p.data[di], true
}

func (p *ComponentPool[T]) Remove(e Entity) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(p.sparse) {
		return
	}
	di := p.sparse[idx]
	if di < 0 {
		return
	}

	last := int32(len(p.data) - 1)
	if di != last {
		p.data[di] = p.data[last]
		p.dense[di] = p.dense[last]
		p.sparse[p.dense[last]] = di
	}

	p.data = p.data[:last]
	p.dense = p.dense[:last]
	p.sparse[idx] = -1
}

func (p *ComponentPool[T]) Each(fn func(Entity, *T)) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for i := range p.data {
		// dense stores raw indices; callers that need generation should
		// look up via EntityManager.Alive() before acting on the entity.
		fn(Entity(p.dense[i]), &p.data[i])
	}
}

func (p *ComponentPool[T]) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.data)
}