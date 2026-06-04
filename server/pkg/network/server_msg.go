package network

import (
	"bytes"
	"encoding/binary"
	"io"
)

// Server message types (server -> client)
const (
	MsgGoldUpdate   uint8 = 0x80
	MsgMatchResult  uint8 = 0x81
	MsgRosterUpdate uint8 = 0x82
)

// ServerMessage is a typed message from server to client.
type ServerMessage struct {
	Type       uint8
	Gold       int32   // MsgGoldUpdate
	Winner     uint8   // MsgMatchResult
	Reason     string  // MsgMatchResult
	RosterData []byte  // MsgRosterUpdate: opaque roster blob
}

// EncodeServerMessage serializes a server message.
// Uses 0xFF magic prefix to distinguish from snapshot data (which starts with tick uint32).
func EncodeServerMessage(msg *ServerMessage) []byte {
	// Magic prefix 0xFF 0xFE to distinguish server messages from snapshot data (tick uint32)
	buf := new(bytes.Buffer)
	buf.WriteByte(0xFF)
	buf.WriteByte(0xFE)
	binary.Write(buf, binary.LittleEndian, msg.Type)

	switch msg.Type {
	case MsgGoldUpdate:
		binary.Write(buf, binary.LittleEndian, msg.Gold)
	case MsgMatchResult:
		binary.Write(buf, binary.LittleEndian, msg.Winner)
		binary.Write(buf, binary.LittleEndian, uint16(len(msg.Reason)))
		buf.WriteString(msg.Reason)
	case MsgRosterUpdate:
		binary.Write(buf, binary.LittleEndian, uint16(len(msg.RosterData)))
		buf.Write(msg.RosterData)
	}
	return buf.Bytes()
}

// DecodeServerMessage deserializes a server message.
// Expects 0xFF magic prefix byte before the type.
func DecodeServerMessage(data []byte) (*ServerMessage, error) {
	r := bytes.NewReader(data)
	// Skip magic prefix (0xFF 0xFE)
	if _, err := r.ReadByte(); err != nil {
		return nil, err
	}
	if _, err := r.ReadByte(); err != nil {
		return nil, err
	}
	msg := &ServerMessage{}
	if err := binary.Read(r, binary.LittleEndian, &msg.Type); err != nil {
		return nil, err
	}

	switch msg.Type {
	case MsgGoldUpdate:
		if err := binary.Read(r, binary.LittleEndian, &msg.Gold); err != nil {
			return nil, err
		}
	case MsgMatchResult:
		if err := binary.Read(r, binary.LittleEndian, &msg.Winner); err != nil {
			return nil, err
		}
		var reasonLen uint16
		if err := binary.Read(r, binary.LittleEndian, &reasonLen); err != nil {
			return nil, err
		}
		reason := make([]byte, reasonLen)
		if _, err := io.ReadFull(r, reason); err != nil {
			return nil, err
		}
		msg.Reason = string(reason)
	case MsgRosterUpdate:
		var dataLen uint16
		if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
			return nil, err
		}
		msg.RosterData = make([]byte, dataLen)
		if _, err := io.ReadFull(r, msg.RosterData); err != nil {
			return nil, err
		}
	}

	if r.Len() > 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return msg, nil
}
