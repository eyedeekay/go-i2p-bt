// Copyright 2020 go-i2p, 2023 idk
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

var errMessageTooLong = fmt.Errorf("the peer message is too long")

// Pieces is used to represent the list of the indexes of the pieces.
type Pieces []uint32

// Sort sorts itself.
func (ps Pieces) Sort()              { sort.Sort(ps) }
func (ps Pieces) Len() int           { return len(ps) }
func (ps Pieces) Less(i, j int) bool { return ps[i] < ps[j] }
func (ps Pieces) Swap(i, j int)      { ps[i], ps[j] = ps[j], ps[i] }

// Append appends the piece index, sorts and returns the new index list.
func (ps Pieces) Append(index uint32) Pieces {
	for _, p := range ps {
		if p == index {
			return ps
		}
	}

	pieces := append(ps, index)
	sort.Sort(pieces)
	return pieces
}

// Remove removes the given piece index from the list and returns new list.
func (ps Pieces) Remove(index uint32) Pieces {
	for i, p := range ps {
		if p == index {
			copy(ps[i:], ps[i+1:])
			return ps[:len(ps)-1]
		}
	}
	return ps
}

// BitField represents the bit field of the pieces.
type BitField []uint8

// NewBitField returns a new BitField to hold the pieceNum pieces.
//
// If set is set to true, it will set all the bit fields to 1.
func NewBitField(pieceNum int, set ...bool) BitField {
	_len := (pieceNum + 7) / 8
	bf := make(BitField, _len)
	if len(set) > 0 && set[0] {
		for i := 0; i < _len; i++ {
			bf[i] = 0xff
		}
	}
	return bf
}

// NewBitFieldFromBools returns a new BitField from the bool list.
func NewBitFieldFromBools(bs []bool) BitField {
	bf := NewBitField(len(bs))
	for i, has := range bs {
		if has {
			bf.Set(uint32(i))
		}
	}
	return bf
}

func (bf BitField) String() string {
	return fmt.Sprintf("%b", bf)
}

// Bools converts the bit field to []bool.
func (bf BitField) Bools() []bool {
	bs := make([]bool, 0, len(bf)*8)
	for _, b := range bf {
		for i := 7; i >= 0; i-- {
			bs = append(bs, (b>>byte(i))&1 == 1)
		}
	}
	return bs
}

// Sets returns the indexes of all the pieces that are set to 1.
func (bf BitField) Sets() (pieces Pieces) {
	_len := len(bf) * 8
	for i := 0; i < _len; i++ {
		index := uint32(i)
		if bf.IsSet(index) {
			pieces = append(pieces, index)
		}
	}
	return
}

// Unsets returns the indexes of all the pieces that are set to 0.
func (bf BitField) Unsets() (pieces Pieces) {
	_len := len(bf) * 8
	for i := 0; i < _len; i++ {
		index := uint32(i)
		if !bf.IsSet(index) {
			pieces = append(pieces, index)
		}
	}
	return
}

// Set sets the bit of the piece to 1 by its index.
func (bf BitField) Set(index uint32) {
	if i := int(index) / 8; i < len(bf) {
		bf[i] |= (1 << byte(7-index%8))
	}
}

// Unset sets the bit of the piece to 0 by its index.
func (bf BitField) Unset(index uint32) {
	if i := int(index) / 8; i < len(bf) {
		bf[i] &^= (1 << byte(7-index%8))
	}
}

// IsSet reports whether the bit of the piece is set to 1.
func (bf BitField) IsSet(index uint32) (set bool) {
	if i := int(index) / 8; i < len(bf) {
		set = bf[i]&(1<<byte(7-index%8)) != 0
	}
	return
}

// Message is the message used by the peer protocol, which contains
// all the fields specified by the standard message types.
type Message struct {
	Keepalive bool
	Type      MessageType

	// Index is used by these message types:
	//
	//   BEP 3: Cancel, Request, Have, Piece
	//   BEP 6: Reject, Suggest, AllowedFast
	//
	Index uint32

	// Begin is used by these message types:
	//
	//   BEP 3: Request, Cancel, Piece
	//   BEP 6: Reject
	//
	Begin uint32

	// Length is used by these message types:
	//
	//   BEP 3: Request, Cancel
	//   BEP 6: Reject
	//
	Length uint32

	// Piece is used by these message types:
	//
	//   BEP 3: Piece
	Piece []byte

	// BitField is used by these message types:
	//
	//   BEP 3: Bitfield
	BitField BitField

	// ExtendedID and ExtendedPayload are used by these message types:
	//
	//   BEP 10: Extended
	//
	ExtendedID      uint8
	ExtendedPayload []byte

	// Port is used by these message types:
	//
	//   BEP 5: Port
	//
	Port uint16

	// UnknownTypePayload is the payload of the unknown message type.
	UnknownTypePayload []byte
}

// DecodeToMessage is equal to msg.Decode(r, maxLength).
func DecodeToMessage(r io.Reader, maxLength uint32) (msg Message, err error) {
	err = msg.Decode(r, maxLength)
	return
}

// UtMetadataExtendedMsg decodes the extended payload as UtMetadataExtendedMsg.
//
// Notice: the message type must be Extended.
func (m Message) UtMetadataExtendedMsg() (um UtMetadataExtendedMsg, err error) {
	if m.Type != MTypeExtended {
		panic("the message type is Extended")
	}
	err = um.DecodeFromPayload(m.ExtendedPayload)
	return
}

// UnmarshalBinary implements the interface encoding.BinaryUnmarshaler,
// which is equal to m.Decode(bytes.NewBuffer(data), 0).
func (m *Message) UnmarshalBinary(data []byte) (err error) {
	return m.Decode(bytes.NewBuffer(data), 0)
}

func readByte(r io.Reader) (b byte, err error) {
	var bs [1]byte
	if _, err = r.Read(bs[:]); err == nil {
		b = bs[0]
	}
	return
}

// Decode reads the data from r and decodes it to Message.
//
// if maxLength is equal to 0, it is unlimited. Or, it will read maxLength bytes
// at most.
func (m *Message) Decode(r io.Reader, maxLength uint32) (err error) {
	length, err := m.readMessageLength(r)
	if err != nil {
		return err
	}

	if length == 0 {
		m.Keepalive = true
		return nil
	}

	if err = m.validateMessageLength(length, maxLength); err != nil {
		return err
	}

	m.Keepalive = false
	lr := &io.LimitedReader{R: r, N: int64(length)}

	// Check that all of r was utilized.
	defer func() {
		if err == nil && lr.N != 0 {
			err = fmt.Errorf("%d bytes unused in message type %d", lr.N, m.Type)
		}
	}()

	messageType, err := readByte(lr)
	if err != nil {
		return err
	}

	m.Type = MessageType(messageType)
	return m.decodeByMessageType(lr, length)
}

// readMessageLength reads and validates the message length from the reader.
func (m *Message) readMessageLength(r io.Reader) (uint32, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		if err != io.EOF {
			return 0, fmt.Errorf("reading length error: %s", err)
		}
		return 0, err
	}
	return length, nil
}

// validateMessageLength checks if the message length is within acceptable limits.
func (m *Message) validateMessageLength(length, maxLength uint32) error {
	if maxLength > 0 && length > maxLength {
		return errMessageTooLong
	}
	return nil
}

// decodeByMessageType decodes the message payload based on its type.
func (m *Message) decodeByMessageType(lr *io.LimitedReader, length uint32) error {
	switch m.Type {
	case MTypeChoke, MTypeUnchoke, MTypeInterested, MTypeNotInterested,
		MTypeHaveAll, MTypeHaveNone:
		return nil
	case MTypeHave, MTypeAllowedFast, MTypeSuggest:
		return m.decodeIndexOnlyMessage(lr)
	case MTypeRequest, MTypeCancel, MTypeReject:
		return m.decodeRequestLikeMessage(lr)
	case MTypeBitField:
		return m.decodeBitFieldMessage(lr, length)
	case MTypePiece:
		return m.decodePieceMessage(lr)
	case MTypeExtended:
		return m.decodeExtendedMessage(lr)
	case MTypePort:
		return m.decodePortMessage(lr)
	default:
		return m.decodeUnknownMessage(lr)
	}
}

// decodeIndexOnlyMessage decodes messages that contain only an index field.
func (m *Message) decodeIndexOnlyMessage(lr *io.LimitedReader) error {
	return binary.Read(lr, binary.BigEndian, &m.Index)
}

// decodeRequestLikeMessage decodes request, cancel, and reject messages.
func (m *Message) decodeRequestLikeMessage(lr *io.LimitedReader) error {
	if err := binary.Read(lr, binary.BigEndian, &m.Index); err != nil {
		return err
	}
	if err := binary.Read(lr, binary.BigEndian, &m.Begin); err != nil {
		return err
	}
	return binary.Read(lr, binary.BigEndian, &m.Length)
}

// decodeBitFieldMessage decodes bitfield messages.
func (m *Message) decodeBitFieldMessage(lr *io.LimitedReader, length uint32) error {
	_len := length - 1
	bs := make([]byte, _len)
	if _, err := io.ReadFull(lr, bs); err == nil {
		m.BitField = BitField(bs)
	}
	return nil
}

// decodePieceMessage decodes piece messages containing data blocks.
func (m *Message) decodePieceMessage(lr *io.LimitedReader) error {
	if err := binary.Read(lr, binary.BigEndian, &m.Index); err != nil {
		return err
	}
	if err := binary.Read(lr, binary.BigEndian, &m.Begin); err != nil {
		return err
	}

	// TODO: Should we use a []byte pool?
	m.Piece = make([]byte, lr.N)
	if _, err := io.ReadFull(lr, m.Piece); err != nil {
		return fmt.Errorf("reading piece data error: %s", err)
	}
	return nil
}

// decodeExtendedMessage decodes extended protocol messages.
func (m *Message) decodeExtendedMessage(lr *io.LimitedReader) error {
	extendedID, err := readByte(lr)
	if err != nil {
		return err
	}
	m.ExtendedID = extendedID
	m.ExtendedPayload, err = io.ReadAll(lr)
	return err
}

// decodePortMessage decodes port messages.
func (m *Message) decodePortMessage(lr *io.LimitedReader) error {
	return binary.Read(lr, binary.BigEndian, &m.Port)
}

// decodeUnknownMessage handles unknown message types.
func (m *Message) decodeUnknownMessage(lr *io.LimitedReader) error {
	var err error
	m.UnknownTypePayload, err = io.ReadAll(lr)
	return err
}

// MarshalBinary implements the interface encoding.BinaryMarshaler.
func (m Message) MarshalBinary() (data []byte, err error) {
	// TODO: Should we use a buffer pool?
	buf := bytes.NewBuffer(make([]byte, 0, 4))
	if err = m.Encode(buf); err == nil {
		data = buf.Bytes()
	}
	return
}

// Encode encodes the message to buf.
func (m Message) Encode(buf *bytes.Buffer) (err error) {
	// The 4-bytes is the placeholder of the length.
	buf.Reset()
	buf.Write([]byte{0, 0, 0, 0})

	// Write the non-keepalive message.
	if !m.Keepalive {
		if err = buf.WriteByte(byte(m.Type)); err != nil {
			return
		} else if err = m.marshalBinaryType(buf); err != nil {
			return
		}

		// Calculate and reset the length of the message body.
		data := buf.Bytes()
		if payloadLen := len(data) - 4; payloadLen > 0 {
			binary.BigEndian.PutUint32(data[:4], uint32(payloadLen))
		}
	}

	return
}

func (m Message) marshalBinaryType(buf *bytes.Buffer) (err error) {
	switch m.Type {
	case MTypeChoke, MTypeUnchoke, MTypeInterested, MTypeNotInterested,
		MTypeHaveAll, MTypeHaveNone:
	case MTypeHave:
		err = binary.Write(buf, binary.BigEndian, m.Index)
	case MTypeRequest, MTypeCancel, MTypeReject:
		if err = binary.Write(buf, binary.BigEndian, m.Index); err != nil {
			return
		}
		if err = binary.Write(buf, binary.BigEndian, m.Begin); err != nil {
			return
		}
		if err = binary.Write(buf, binary.BigEndian, m.Length); err != nil {
			return
		}
	case MTypeBitField:
		buf.Write(m.BitField)
	case MTypePiece:
		if err = binary.Write(buf, binary.BigEndian, m.Index); err != nil {
			return
		}
		if err = binary.Write(buf, binary.BigEndian, m.Begin); err != nil {
			return
		}
		_, err = buf.Write(m.Piece)
	case MTypeExtended:
		if err = buf.WriteByte(byte(m.ExtendedID)); err != nil {
			_, err = buf.Write(m.ExtendedPayload)
		}
	case MTypePort:
		err = binary.Write(buf, binary.BigEndian, m.Port)
	default:
		// err = fmt.Errorf("unknown message type: %v", m.Type)
		_, err = buf.Write(m.UnknownTypePayload)
	}

	return
}
