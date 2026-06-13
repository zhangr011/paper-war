package network

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	CmdMoveSquad       uint8 = 0x01
	CmdAttackTarget    uint8 = 0x02
	CmdAttackGround    uint8 = 0x03
	CmdChangeFormation uint8 = 0x04
	CmdTacticalOrder   uint8 = 0x05
	CmdRecruit         uint8 = 0x06
	CmdSelectCommander uint8 = 0x07
	CmdBuild           uint8 = 0x08
)

type Command struct {
	Type          uint8
	ClientSeq     uint32
	PredictedTick uint32
	SquadID       uint32
	TargetX       int32
	TargetY       int32
	TargetID      uint32
	FormationType uint8
	OrderType     uint8
	RecruitType   uint8 // CombatUnitType for CmdRecruit
}

func EncodeCommand(cmd *Command) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, cmd.Type)
	binary.Write(buf, binary.LittleEndian, cmd.ClientSeq)
	binary.Write(buf, binary.LittleEndian, cmd.PredictedTick)
	binary.Write(buf, binary.LittleEndian, cmd.SquadID)

	switch cmd.Type {
	case CmdMoveSquad, CmdAttackGround:
		binary.Write(buf, binary.LittleEndian, cmd.TargetX)
		binary.Write(buf, binary.LittleEndian, cmd.TargetY)
	case CmdAttackTarget:
		binary.Write(buf, binary.LittleEndian, cmd.TargetID)
	case CmdChangeFormation:
		binary.Write(buf, binary.LittleEndian, cmd.FormationType)
	case CmdTacticalOrder:
		binary.Write(buf, binary.LittleEndian, cmd.OrderType)
	case CmdRecruit:
		binary.Write(buf, binary.LittleEndian, cmd.RecruitType)
	case CmdSelectCommander:
		// SquadID already written in header
	case CmdBuild:
		binary.Write(buf, binary.LittleEndian, cmd.RecruitType) // structure type
		binary.Write(buf, binary.LittleEndian, cmd.TargetX)
		binary.Write(buf, binary.LittleEndian, cmd.TargetY)
	}
	return buf.Bytes()
}

func DecodeCommand(data []byte) (*Command, error) {
	r := bytes.NewReader(data)
	cmd := &Command{}
	if err := binary.Read(r, binary.LittleEndian, &cmd.Type); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &cmd.ClientSeq); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &cmd.PredictedTick); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &cmd.SquadID); err != nil {
		return nil, err
	}

	switch cmd.Type {
	case CmdMoveSquad, CmdAttackGround:
		if err := binary.Read(r, binary.LittleEndian, &cmd.TargetX); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &cmd.TargetY); err != nil {
			return nil, err
		}
	case CmdAttackTarget:
		if err := binary.Read(r, binary.LittleEndian, &cmd.TargetID); err != nil {
			return nil, err
		}
	case CmdChangeFormation:
		if err := binary.Read(r, binary.LittleEndian, &cmd.FormationType); err != nil {
			return nil, err
		}
	case CmdTacticalOrder:
		if err := binary.Read(r, binary.LittleEndian, &cmd.OrderType); err != nil {
			return nil, err
		}
	case CmdRecruit:
		if err := binary.Read(r, binary.LittleEndian, &cmd.RecruitType); err != nil {
			return nil, err
		}
		// SquadID already read above
	case CmdSelectCommander:
		// SquadID already read above
	case CmdBuild:
		if err := binary.Read(r, binary.LittleEndian, &cmd.RecruitType); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &cmd.TargetX); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &cmd.TargetY); err != nil {
			return nil, err
		}
	}

	// Check no unread data
	if r.Len() > 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return cmd, nil
}
