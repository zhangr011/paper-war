package network

import (
	"encoding/binary"
)

const (
	ChangedPosition uint8 = 1 << iota
	ChangedVelocity
	ChangedAngle
	ChangedHP
	ChangedTargetID
	ChangedMorale
	ChangedState
	ChangedSquadID
)

// MaskFull is sent for brand-new units to indicate all fields are present.
const MaskFull uint8 = 0xFF

type UnitSnapshot struct {
	EntityID    uint32
	ChangedMask uint8
	X, Y        int64
	Vx, Vy      int64
	Angle       int16
	HP          int32
	TargetID    uint32
	Morale      int32
	State       uint8
	SquadID     uint32
	UnitType    uint8 // 0-6: one of 7 CombatUnitType values (always sent for new units)
	Team        uint8 // player/faction ID (always sent for new units)
}

type EventType uint8

const (
	EventDamage        EventType = 0
	EventDeath         EventType = 1
	EventTerrainChange EventType = 2
	EventCommanderDown EventType = 3
	EventProjectile    EventType = 4
)

type Event struct {
	Type EventType
	Data []byte
}

type Snapshot struct {
	Tick     uint32
	PrevTick uint32
	Units    []UnitSnapshot
	Events   []Event
}

type EntityState struct {
	X, Y     int64
	Vx, Vy   int64
	Angle    int16
	HP       int32
	TargetID uint32
	Morale   int32
	State    uint8
	SquadID  uint32
	UnitType uint8
	Team     uint8
}

type SnapshotGenerator struct {
	prevStates map[uint32]EntityState
	lastTick   uint32
	thresholds struct {
		position int64
		velocity int64
		angle    int16
		hp       int32
		morale   int32
	}
}

func NewSnapshotGenerator() *SnapshotGenerator {
	sg := &SnapshotGenerator{
		prevStates: make(map[uint32]EntityState),
	}
	sg.thresholds.position = 10 // ~0.002 world units
	sg.thresholds.velocity = 5
	sg.thresholds.angle = 5 // ~0.5 degrees
	sg.thresholds.hp = 1
	sg.thresholds.morale = 1
	return sg
}

func diff(a, b int64) int64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}

func (sg *SnapshotGenerator) Generate(tick uint32, units []EntityState, ids []uint32) *Snapshot {
	snap := &Snapshot{
		Tick:     tick,
		PrevTick: sg.lastTick,
	}

	for i, id := range ids {
		cur := units[i]
		prev, exists := sg.prevStates[id]
		if !exists {
			// New unit — send all fields
			snap.Units = append(snap.Units, UnitSnapshot{
				EntityID:    id,
				ChangedMask: MaskFull,
				X:           cur.X, Y: cur.Y,
				Vx: cur.Vx, Vy: cur.Vy,
				Angle:    cur.Angle,
				HP:       cur.HP,
				TargetID: cur.TargetID,
				Morale:   cur.Morale,
				State:    cur.State,
				SquadID:  cur.SquadID,
				UnitType: cur.UnitType,
				Team:     cur.Team,
			})
		} else {
			mask := uint8(0)
			if diff(cur.X, prev.X) > sg.thresholds.position || diff(cur.Y, prev.Y) > sg.thresholds.position {
				mask |= ChangedPosition
			}
			if diff(cur.Vx, prev.Vx) > sg.thresholds.velocity || diff(cur.Vy, prev.Vy) > sg.thresholds.velocity {
				mask |= ChangedVelocity
			}
			if cur.Angle != prev.Angle {
				d := cur.Angle - prev.Angle
				if d < 0 {
					d = -d
				}
				if d > sg.thresholds.angle {
					mask |= ChangedAngle
				}
			}
			if cur.HP != prev.HP {
				d := cur.HP - prev.HP
				if d < 0 {
					d = -d
				}
				if d >= sg.thresholds.hp {
					mask |= ChangedHP
				}
			}
			if cur.TargetID != prev.TargetID {
				mask |= ChangedTargetID
			}
			if cur.Morale != prev.Morale {
				d := cur.Morale - prev.Morale
				if d < 0 {
					d = -d
				}
				if d >= sg.thresholds.morale {
					mask |= ChangedMorale
				}
			}
			if cur.State != prev.State {
				mask |= ChangedState
			}
			if cur.SquadID != prev.SquadID {
				mask |= ChangedSquadID
			}

			if mask > 0 {
				snap.Units = append(snap.Units, UnitSnapshot{
					EntityID:    id,
					ChangedMask: mask,
					X:           cur.X, Y: cur.Y,
					Vx: cur.Vx, Vy: cur.Vy,
					Angle:    cur.Angle,
					HP:       cur.HP,
					TargetID: cur.TargetID,
					Morale:   cur.Morale,
					State:    cur.State,
					SquadID:  cur.SquadID,
				})
			}
		}

		sg.prevStates[id] = cur
	}

	sg.lastTick = tick
	return snap
}

// EncodeSnapshot serializes a snapshot to binary.
func EncodeSnapshot(snap *Snapshot) []byte {
	size := 4 + 4 + 2 + 1 // tick + prevtick + unitcount + eventcount
	// Estimate unit data
	for _, u := range snap.Units {
		size += 4 + 1 // entityID + mask
		if u.ChangedMask&ChangedPosition != 0 {
			size += 16
		}
		if u.ChangedMask&ChangedVelocity != 0 {
			size += 16
		}
		if u.ChangedMask&ChangedAngle != 0 {
			size += 2
		}
		if u.ChangedMask&ChangedHP != 0 {
			size += 4
		}
		if u.ChangedMask&ChangedTargetID != 0 {
			size += 4
		}
		if u.ChangedMask&ChangedMorale != 0 {
			size += 4
		}
		if u.ChangedMask&ChangedState != 0 {
			size += 1
		}
		if u.ChangedMask&ChangedSquadID != 0 {
			size += 4
		}
		// UnitType+Team: only for new units (MaskFull = 0xFF)
		if u.ChangedMask == MaskFull {
			size += 2
		}
	}

	buf := make([]byte, 0, size)
	buf = appendUint32(buf, snap.Tick)
	buf = appendUint32(buf, snap.PrevTick)
	buf = appendUint16(buf, uint16(len(snap.Units)))
	buf = append(buf, uint8(len(snap.Events)))

	for _, u := range snap.Units {
		buf = appendUint32(buf, u.EntityID)
		buf = append(buf, u.ChangedMask)
		if u.ChangedMask&ChangedPosition != 0 {
			buf = appendUint64(buf, uint64(u.X))
			buf = appendUint64(buf, uint64(u.Y))
		}
		if u.ChangedMask&ChangedVelocity != 0 {
			buf = appendUint64(buf, uint64(u.Vx))
			buf = appendUint64(buf, uint64(u.Vy))
		}
		if u.ChangedMask&ChangedAngle != 0 {
			buf = appendUint16(buf, uint16(u.Angle))
		}
		if u.ChangedMask&ChangedHP != 0 {
			buf = appendUint32(buf, uint32(u.HP))
		}
		if u.ChangedMask&ChangedTargetID != 0 {
			buf = appendUint32(buf, u.TargetID)
		}
		if u.ChangedMask&ChangedMorale != 0 {
			buf = appendUint32(buf, uint32(u.Morale))
		}
		if u.ChangedMask&ChangedState != 0 {
			buf = append(buf, u.State)
		}
		if u.ChangedMask&ChangedSquadID != 0 {
			buf = appendUint32(buf, u.SquadID)
		}
		// UnitType+Team: only for new units
		if u.ChangedMask == MaskFull {
			buf = append(buf, u.UnitType, u.Team)
		}
	}
	return buf
}

func appendUint32(buf []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(buf, v)
}
func appendUint16(buf []byte, v uint16) []byte {
	return binary.LittleEndian.AppendUint16(buf, v)
}
func appendUint64(buf []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(buf, v)
}
