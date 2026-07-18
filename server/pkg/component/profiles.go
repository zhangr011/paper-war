package component

// StandardMovementProfiles returns the two v1 movement profiles:
//   - Profile 0 (Light): can traverse all terrain including shallow water
//   - Profile 1 (Heavy): cannot cross shallow or deep water
func StandardMovementProfiles() []*MovementProfile {
	return []*MovementProfile{
		{ // Light profile (ID 0)
			ID: 0,
			TerrainCosts: [18]uint8{
				1,  // Plain
				1,  // Road
				2,  // Shallow - passable
				0,  // Deep - impassable
				2,  // Forest
				3,  // Hill
				3,  // Swamp
				1,  // Bridge
				0,  // Wall - impassable
				2,  // Snow
				2,  // Desert
				1,  // Stronghold1
				1,  // Stronghold2
				1,  // Stronghold3
				1,  // Stronghold4
				1,  // Stronghold5
				4,  // Rock - passable but slow for Light (clambering over crags)
				1,  // Brush - trivial for Light
			},
		},
		{ // Heavy profile (ID 1)
			ID: 1,
			TerrainCosts: [18]uint8{
				1,  // Plain
				1,  // Road
				0,  // Shallow - impassable for Heavy
				0,  // Deep - impassable
				3,  // Forest - slower for Heavy
				4,  // Hill - slower for Heavy
				4,  // Swamp - slower for Heavy
				1,  // Bridge
				0,  // Wall - impassable
				3,  // Snow - slower for Heavy
				2,  // Desert
				1,  // Stronghold1
				1,  // Stronghold2
				1,  // Stronghold3
				1,  // Stronghold4
				1,  // Stronghold5
				5,  // Rock - passable but very slow for Heavy (avoids cutting Heavy routes)
				2,  // Brush - slower for Heavy
			},
		},
	}
}

// ArmorTypeToProfileID maps an ArmorType to its MovementProfile ID.
func ArmorTypeToProfileID(armor ArmorType) uint8 {
	switch armor {
	case ArmorHeavy:
		return 1 // Heavy profile
	default:
		return 0 // Light profile
	}
}
