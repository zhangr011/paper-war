
package game

import (
    "testing"
    "github.com/user/paper-war/server/pkg/component"
    "github.com/user/paper-war/server/pkg/ecs"
    "github.com/user/paper-war/server/pkg/fixed"
)

func TestFormationOffsetsRawValues(t *testing.T) {
    gs := NewGameSession()
    gs.SpawnTeamWithType(1, 1, fixed.FromFloat(10), fixed.FromFloat(10), 1, component.UnitLightInfantry)
    
    frPool := gs.World.Pool(component.FormationRoleComponent{}).(*ecs.ComponentPool[component.FormationRoleComponent])
    boidPool := gs.World.Pool(component.BoidComponent{}).(*ecs.ComponentPool[component.BoidComponent])
    posPool := gs.World.Pool(component.PositionComponent{}).(*ecs.ComponentPool[component.PositionComponent])
    
    commanderPos := fixed.FromFloat(10)
    spacing := fixed.FromFloat(1.2)
    
    frPool.Each(func(e ecs.Entity, fr *component.FormationRoleComponent) {
        bc, _ := boidPool.Get(e)
        if bc.Role == component.RoleCommander {
            return
        }
        pos, _ := posPool.Get(e)
        t.Logf("unit %d: offsetX=%d offsetY=%d posX=%d posY=%d dxFromCmd=%d dyFromCmd=%d",
            e, fr.OffsetX, fr.OffsetY, pos.X, pos.Y,
            pos.X - commanderPos, pos.Y - commanderPos)
        
        // Check spacing is meaningful
        oxTiles := fixed.ToFloat(fr.OffsetX)
        oyTiles := fixed.ToFloat(fr.OffsetY)
        t.Logf("  offset: %.2f x %.2f tiles", oxTiles, oyTiles)
        t.Logf("  spacing value: %d (=%.2f tiles)", spacing, fixed.ToFloat(spacing))
    })
    
    // Also log what the grid calc would be
    unitCount := 5
    cols := 1
    for cols*cols < unitCount {
        cols++
    }
    t.Logf("grid: %d units, cols=%d", unitCount, cols)
    for i := 0; i < unitCount; i++ {
        row := i / cols
        col := i % cols
        ox := fixed.Mul(int64(col-(cols-1)/2), spacing)
        oy := fixed.Mul(int64(row+1), spacing)
        t.Logf("  i=%d: col=%d row=%d ox=%d(=%.2f) oy=%d(=%.2f)", 
            i, col, row, ox, fixed.ToFloat(ox), oy, fixed.ToFloat(oy))
    }
}
