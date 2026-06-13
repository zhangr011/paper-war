package component

// StructureType identifies a buildable defensive structure.
type StructureType uint8

const (
	StructureWatchtower StructureType = 1
	StructureBarricade  StructureType = 2
	StructureTurret     StructureType = 3
)

// StructureStat defines the properties of a defensive structure.
type StructureStat struct {
	Type     StructureType
	HP       int32
	Cost     int32 // gold cost
	Range    int32 // tiles (turret attack range, watchtower vision range)
	Damage   int32 // turret attack damage per shot
	Cooldown uint32 // turret attack cooldown in ticks
	Name     string
}

// StructureTypeTable holds the stats for each structure type.
var StructureTypeTable = map[StructureType]StructureStat{
	StructureWatchtower: {
		Type:   StructureWatchtower,
		HP:     100,
		Cost:   50,
		Range:  8,
		Name:   "Watchtower",
	},
	StructureBarricade: {
		Type:   StructureBarricade,
		HP:     50,
		Cost:   20,
		Name:   "Barricade",
	},
	StructureTurret: {
		Type:     StructureTurret,
		HP:       150,
		Cost:     80,
		Range:    5,
		Damage:   15,
		Cooldown: 10, // 2 seconds at 5 Hz
		Name:     "Turret",
	},
}

// StructureComponent marks an entity as a buildable structure.
type StructureComponent struct {
	Type     StructureType
	OwnerID  uint32 // player who built it
	LastFire uint32 // turret last attack tick
}
