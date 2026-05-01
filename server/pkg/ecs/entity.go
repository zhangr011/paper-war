package ecs

import "sync"

type Entity uint64

const InvalidEntity Entity = 0

func entityGen(e Entity) uint32 { return uint32(e >> 32) }
func entityIdx(e Entity) uint32 { return uint32(e & 0xFFFFFFFF) }
func makeEntity(idx, gen uint32) Entity {
	return Entity(uint64(gen)<<32 | uint64(idx))
}

type EntityManager struct {
	mu          sync.Mutex
	generations []uint32
	freeList    []uint32
	nextIdx     uint32
	maxEntities uint32
}

func NewEntityManager() *EntityManager {
	em := NewEntityManagerWithMax(1 << 20)
	em.nextIdx = 1 // Start entities from 1, not 0
	return em
}

func NewEntityManagerWithMax(max uint32) *EntityManager {
	return &EntityManager{
		generations: make([]uint32, 0, 1024),
		freeList:    make([]uint32, 0, 256),
		maxEntities: max,
	}
}

func (em *EntityManager) Create() Entity {
	em.mu.Lock()
	defer em.mu.Unlock()

	var idx uint32
	if len(em.freeList) > 0 {
		idx = em.freeList[len(em.freeList)-1]
		em.freeList = em.freeList[:len(em.freeList)-1]
	} else {
		idx = em.nextIdx
		if idx >= em.maxEntities {
			return InvalidEntity
		}
		em.nextIdx++
		if int(idx) >= len(em.generations) {
			em.generations = append(em.generations, make([]uint32, idx+1-uint32(len(em.generations)))...)
		}
	}

	gen := em.generations[idx]
	return makeEntity(idx, gen)
}

func (em *EntityManager) Destroy(e Entity) {
	em.mu.Lock()
	defer em.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(em.generations) {
		return
	}
	if em.generations[idx] != entityGen(e) {
		return
	}
	em.generations[idx]++
	em.freeList = append(em.freeList, idx)
}

func (em *EntityManager) Alive(e Entity) bool {
	em.mu.Lock()
	defer em.mu.Unlock()

	idx := entityIdx(e)
	if int(idx) >= len(em.generations) {
		return false
	}
	return em.generations[idx] == entityGen(e)
}