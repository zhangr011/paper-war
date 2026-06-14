package component

type TacticalState uint8

const (
	TacticalFollow  TacticalState = 0
	TacticalCharge  TacticalState = 1
	TacticalRetreat TacticalState = 2
	TacticalHold    TacticalState = 3
)

type CommanderComponent struct {
	SquadID         uint32
	AuraRadius      int64
	AuraMoraleBonus int32
	TacticalState   TacticalState
	IsAlive         bool
	Promoted        bool // true if promoted from combat unit (not original commander)
}