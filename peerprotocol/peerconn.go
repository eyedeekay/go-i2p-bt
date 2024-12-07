// Copyright 2020 xgfone, 2023 idk
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
	"fmt"
	"io"
	"net"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// Predefine some errors about extension support.
var (
	ErrChoked             = fmt.Errorf("choked")
	ErrNotFirstMsg        = fmt.Errorf("not the first message")
	ErrNotSupportDHT      = fmt.Errorf("not support DHT extension")
	ErrNotSupportFast     = fmt.Errorf("not support Fast extension")
	ErrNotSupportExtended = fmt.Errorf("not support Extended extension")
	ErrSecondExtHandshake = fmt.Errorf("second extended handshake")
	ErrNoExtHandshake     = fmt.Errorf("no extended handshake")
)

var DialTimeout = net.DialTimeout
var Dial = net.Dial

// Bep3Handler is used to handle the BEP 3 type message if Handler has also
// implemented the interface.
type Bep3Handler interface {
	Choke(pc *PeerConn) error
	Unchoke(pc *PeerConn) error
	Interested(pc *PeerConn) error
	NotInterested(pc *PeerConn) error
	Have(pc *PeerConn, index uint32) error
	BitField(pc *PeerConn, bits BitField) error
	Request(pc *PeerConn, index uint32, begin uint32, length uint32) error
	Piece(pc *PeerConn, index uint32, begin uint32, piece []byte) error
	Cancel(pc *PeerConn, index uint32, begin uint32, length uint32) error
}

// Bep5Handler is used to handle the BEP 5 type message if Handler has also
// implemented the interface.
//
// Notice: the server must enable the DHT extension bit.
type Bep5Handler interface {
	Port(pc *PeerConn, port uint16) error
}

// Bep6Handler is used to handle the BEP 6 type message if Handler has also
// implemented the interface.
//
// Notice: the server must enable the Fast extension bit.
type Bep6Handler interface {
	HaveAll(pc *PeerConn) error
	HaveNone(pc *PeerConn) error
	Suggest(pc *PeerConn, index uint32) error
	AllowedFast(pc *PeerConn, index uint32) error
	Reject(pc *PeerConn, index uint32, begin uint32, length uint32) error
}

// Bep10Handler is used to handle the BEP 10 extended peer message
// if Handler has also implemented the interface.
//
// Notice: the server must enable the Extended extension bit.
type Bep10Handler interface {
	OnExtHandShake(conn *PeerConn) error
	OnPayload(conn *PeerConn, extid uint8, payload []byte) error
}

// PeerConn is used to manage the connection to the peer.
type PeerConn struct {
	net.Conn

	InfoHash metainfo.Hash

	ID      metainfo.Hash // The ID of the local peer.
	ExtBits ExtensionBits // The extension bits of the local peer.

	PeerID      metainfo.Hash // The ID of the remote peer.
	PeerExtBits ExtensionBits // The extension bits of the remote peer.

	// These two states is controlled by the local client peer.
	//
	// Choked is used to indicate whether or not the local client has choked
	// the remote peer, that's, if Choked is true, the local client will
	// discard all the pending requests from the remote peer and not answer
	// any requests until the local client is unchoked.
	//
	// Interested is used to indicate whether or not the local client is
	// interested in something the remote peer has to offer, that's,
	// if Interested is true, the local client will begin requesting blocks
	// when the remote client unchokes them.
	//
	Choked     bool // The default should be true.
	Interested bool

	// These two states is controlled by the remote peer.
	//
	// PeerChoked is used to indicate whether or not the remote peer has choked
	// the local client, that's, if PeerChoked is true, the remote peer will
	// discard all the pending requests from the local client and not answer
	// any requests until the remote peer is unchoked.
	//
	// PeerInterested is used to indicate whether or not the remote peer is
	// interested in something the local client has to offer, that's,
	// if PeerInterested is true, the remote peer will begin requesting blocks
	// when the local client unchokes them.
	//
	PeerChoked     bool // The default should be true.
	PeerInterested bool

	// Timeout is used to control the timeout of reading/writing the message.
	//
	// The default is 0, which represents no timeout.
	Timeout time.Duration

	// MaxLength is used to limit the maximum number of the message body.
	//
	// The default is 0, which represents no limit.
	MaxLength uint32

	Fasts    Pieces   // The list of the indexes of the FAST pieces.
	Suggests Pieces   // The list of the indexes of the SUGGEST pieces.
	BitField BitField // The bit field of the piece indexes.

	// ExtendedHandshakeMsg is the handshake message of the extension
	// from the remote peer.
	ExtendedHandshakeMsg ExtendedHandshakeMsg

	// Data is used to store the context data associated with the connection.
	Data interface{}

	// OnWriteMsg is called when sending a message to the remote peer.
	// You can use it to queue the sent messages.
	//
	// Optional.
	OnWriteMsg func(pc *PeerConn, m Message) error

	notFirstMsg  bool
	extHandshake bool

	PEXID uint8 // 0 means not supported
}

// NewPeerConn returns a new PeerConn.
//
// Notice: conn and id must not be empty, and infohash may be empty
// for the peer server, but not for the peer client.
func NewPeerConn(conn net.Conn, id, infohash metainfo.Hash) *PeerConn {
	return &PeerConn{
		Conn:       conn,
		ID:         id,
		InfoHash:   infohash,
		Choked:     true,
		PeerChoked: true,
		MaxLength:  256 * 1024, // 256KB
	}
}

// NewPeerConnByDial returns a new PeerConn by dialing to addr with the "tcp" network.
func NewPeerConnByDial(addr string, id, infohash metainfo.Hash, timeout time.Duration) (pc *PeerConn, err error) {
	conn, err := DialTimeout("tcp", addr, timeout)
	if err == nil {
		pc = NewPeerConn(conn, id, infohash)
	}
	return
}

func (pc *PeerConn) setReadTimeout() {
	if pc.Timeout > 0 {
		pc.Conn.SetReadDeadline(time.Now().Add(pc.Timeout))
	}
}

func (pc *PeerConn) setWriteTimeout() {
	if pc.Timeout > 0 {
		pc.Conn.SetWriteDeadline(time.Now().Add(pc.Timeout))
	}
}

// SetChoked sets the Choked state of the local client peer.
//
// Notice: if the current state is not Choked, it will send a Choked message
// to the remote peer.
func (pc *PeerConn) SetChoked() (err error) {
	if !pc.Choked {
		if err = pc.SendChoke(); err == nil {
			pc.Choked = true
		}
	}
	return
}

// SetUnchoked sets the Unchoked state of the local client peer.
//
// Notice: if the current state is not Unchoked, it will send a Unchoked message
// to the remote peer.
func (pc *PeerConn) SetUnchoked() (err error) {
	if pc.Choked {
		if err = pc.SendUnchoke(); err == nil {
			pc.Choked = false
		}
	}
	return
}

// SetInterested sets the Interested state of the local client peer.
//
// Notice: if the current state is not Interested, it will send a Interested
// message to the remote peer.
func (pc *PeerConn) SetInterested() (err error) {
	if !pc.Interested {
		if err = pc.SendInterested(); err == nil {
			pc.Interested = true
		}
	}
	return
}

// SetNotInterested sets the NotInterested state of the local client peer.
//
// Notice: if the current state is not NotInterested, it will send
// a NotInterested message to the remote peer.
func (pc *PeerConn) SetNotInterested() (err error) {
	if pc.Interested {
		if err = pc.SendNotInterested(); err == nil {
			pc.Interested = false
		}
	}
	return
}

// Handshake does a handshake with the peer.
//
// BEP 3
func (pc *PeerConn) Handshake() error {
	m := HandshakeMsg{ExtensionBits: pc.ExtBits, PeerID: pc.ID, InfoHash: pc.InfoHash}
	pc.setReadTimeout()
	rhm, err := Handshake(pc.Conn, m)
	if err == nil {
		pc.PeerID = rhm.PeerID
		pc.PeerExtBits = rhm.ExtensionBits
		if pc.InfoHash.IsZero() {
			pc.InfoHash = rhm.InfoHash
		} else if pc.InfoHash != rhm.InfoHash {
			return fmt.Errorf("inconsistent infohash: local(%s)=%s, remote(%s)=%s",
				pc.Conn.LocalAddr().String(), pc.InfoHash.String(),
				pc.Conn.RemoteAddr().String(), rhm.InfoHash.String())
		}
	}
	return err
}

// ReadMsg reads the message.
//
// BEP 3
func (pc *PeerConn) ReadMsg() (m Message, err error) {
	pc.setReadTimeout()
	err = m.Decode(pc.Conn, pc.MaxLength)
	return
}

// WriteMsg writes the message to the peer.
//
// BEP 3
func (pc *PeerConn) WriteMsg(m Message) (err error) {
	if pc.OnWriteMsg != nil {
		return pc.OnWriteMsg(pc, m)
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))
	if err = m.Encode(buf); err == nil {
		pc.setWriteTimeout()

		var n int
		if n, err = pc.Conn.Write(buf.Bytes()); err == nil && n < buf.Len() {
			err = io.ErrShortWrite
		}
	}
	return
}

// SendKeepalive sends a Keepalive message to the peer.
//
// BEP 3
func (pc *PeerConn) SendKeepalive() error {
	return pc.WriteMsg(Message{Keepalive: true})
}

// SendChoke sends a Choke message to the peer.
//
// BEP 3
func (pc *PeerConn) SendChoke() error {
	return pc.WriteMsg(Message{Type: MTypeChoke})
}

// SendUnchoke sends a Unchoke message to the peer.
//
// BEP 3
func (pc *PeerConn) SendUnchoke() error {
	return pc.WriteMsg(Message{Type: MTypeUnchoke})
}

// SendInterested sends a Interested message to the peer.
//
// BEP 3
func (pc *PeerConn) SendInterested() error {
	return pc.WriteMsg(Message{Type: MTypeInterested})
}

// SendNotInterested sends a NotInterested message to the peer.
//
// BEP 3
func (pc *PeerConn) SendNotInterested() error {
	return pc.WriteMsg(Message{Type: MTypeNotInterested})
}

// SendBitfield sends a Bitfield message to the peer.
//
// BEP 3
func (pc *PeerConn) SendBitfield(bits BitField) error {
	return pc.WriteMsg(Message{Type: MTypeBitField, BitField: bits})
}

// SendHave sends a Have message to the peer.
//
// BEP 3
func (pc *PeerConn) SendHave(index uint32) error {
	return pc.WriteMsg(Message{Type: MTypeHave, Index: index})
}

// SendRequest sends a Request message to the peer.
//
// BEP 3
func (pc *PeerConn) SendRequest(index, begin, length uint32) error {
	return pc.WriteMsg(Message{
		Type:   MTypeRequest,
		Index:  index,
		Begin:  begin,
		Length: length,
	})
}

// SendCancel sends a Cancel message to the peer.
//
// BEP 3
func (pc *PeerConn) SendCancel(index, begin, length uint32) error {
	return pc.WriteMsg(Message{
		Type:   MTypeCancel,
		Index:  index,
		Begin:  begin,
		Length: length,
	})
}

// SendPiece sends a Piece message to the peer.
//
// BEP 3
func (pc *PeerConn) SendPiece(index, begin uint32, piece []byte) error {
	return pc.WriteMsg(Message{
		Type:  MTypePiece,
		Index: index,
		Begin: begin,
		Piece: piece,
	})
}

// SendPort sends a Port message to the peer.
//
// BEP 5
func (pc *PeerConn) SendPort(port uint16) error {
	return pc.WriteMsg(Message{Type: MTypePort, Port: port})
}

// SendHaveAll sends a HaveAll message to the peer.
//
// BEP 6
func (pc *PeerConn) SendHaveAll() error {
	return pc.WriteMsg(Message{Type: MTypeHaveAll})
}

// SendHaveNone sends a HaveNone message to the peer.
//
// BEP 6
func (pc *PeerConn) SendHaveNone() error {
	return pc.WriteMsg(Message{Type: MTypeHaveNone})
}

// SendSuggest sends a Suggest message to the peer.
//
// BEP 6
func (pc *PeerConn) SendSuggest(index uint32) error {
	return pc.WriteMsg(Message{Type: MTypeSuggest, Index: index})
}

// SendReject sends a Reject message to the peer.
//
// BEP 6
func (pc *PeerConn) SendReject(index, begin, length uint32) error {
	return pc.WriteMsg(Message{
		Type:   MTypeReject,
		Index:  index,
		Begin:  begin,
		Length: length,
	})
}

// SendAllowedFast sends a AllowedFast message to the peer.
//
// BEP 6
func (pc *PeerConn) SendAllowedFast(index uint32) error {
	return pc.WriteMsg(Message{Type: MTypeAllowedFast, Index: index})
}

// SendExtHandshakeMsg sends the Extended Handshake message to the peer.
//
// BEP 10
func (pc *PeerConn) SendExtHandshakeMsg(m ExtendedHandshakeMsg) (err error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	if err = bencode.NewEncoder(buf).Encode(m); err == nil {
		err = pc.SendExtMsg(ExtendedIDHandshake, buf.Bytes())
	}
	return
}

// SendExtMsg sends the Extended message with the extended id and the payload.
//
// BEP 10
func (pc *PeerConn) SendExtMsg(extID uint8, payload []byte) error {
	return pc.WriteMsg(Message{
		Type:            MTypeExtended,
		ExtendedID:      extID,
		ExtendedPayload: payload,
	})
}

// HandleMessage calls the method of the handler to handle the message.
//
// If handler has also implemented the interfaces Bep3Handler, Bep5Handler,
// Bep6Handler or Bep10Handler, their methods will be called instead of
// Handler.OnMessage for the corresponding type message.
func (pc *PeerConn) HandleMessage(msg Message, handler Handler) (err error) {
	if msg.Keepalive {
		return
	}

	switch msg.Type {
	// BEP 3 - The BitTorrent Protocol Specification
	case MTypeChoke:
		pc.PeerChoked = true
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Choke(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeUnchoke:
		pc.PeerChoked = false
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Unchoke(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeInterested:
		pc.PeerInterested = true
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Interested(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeNotInterested:
		pc.PeerInterested = false
		if h, ok := handler.(Bep3Handler); ok {
			err = h.NotInterested(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeHave:
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Have(pc, msg.Index)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeBitField:
		if pc.notFirstMsg {
			err = ErrNotFirstMsg
		} else {
			pc.BitField = msg.BitField
			if h, ok := handler.(Bep3Handler); ok {
				err = h.BitField(pc, msg.BitField)
			} else {
				err = handler.OnMessage(pc, msg)
			}
		}
	case MTypeRequest:
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Request(pc, msg.Index, msg.Begin, msg.Length)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypePiece:
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Piece(pc, msg.Index, msg.Begin, msg.Piece)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeCancel:
		if h, ok := handler.(Bep3Handler); ok {
			err = h.Cancel(pc, msg.Index, msg.Begin, msg.Length)
		} else {
			err = handler.OnMessage(pc, msg)
		}

	// BEP 5 - DHT Protocol
	case MTypePort:
		if !pc.ExtBits.IsSupportDHT() {
			err = ErrNotSupportDHT
		} else if h, ok := handler.(Bep5Handler); ok {
			err = h.Port(pc, msg.Port)
		} else {
			err = handler.OnMessage(pc, msg)
		}

	// BEP 6 - Fast Extension
	case MTypeSuggest:
		if !pc.ExtBits.IsSupportFast() {
			err = ErrNotSupportFast
		} else {
			pc.Suggests = pc.Suggests.Append(msg.Index)
			if h, ok := handler.(Bep6Handler); ok {
				err = h.Suggest(pc, msg.Index)
			} else {
				err = handler.OnMessage(pc, msg)
			}
		}
	case MTypeHaveAll:
		if pc.notFirstMsg {
			err = ErrNotFirstMsg
		} else if !pc.ExtBits.IsSupportFast() {
			err = ErrNotSupportFast
		} else if h, ok := handler.(Bep6Handler); ok {
			err = h.HaveAll(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeHaveNone:
		if pc.notFirstMsg {
			err = ErrNotFirstMsg
		} else if !pc.ExtBits.IsSupportFast() {
			err = ErrNotSupportFast
		} else if h, ok := handler.(Bep6Handler); ok {
			err = h.HaveNone(pc)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeReject:
		if !pc.ExtBits.IsSupportFast() {
			err = ErrNotSupportFast
		} else if h, ok := handler.(Bep6Handler); ok {
			err = h.Reject(pc, msg.Index, msg.Begin, msg.Length)
		} else {
			err = handler.OnMessage(pc, msg)
		}
	case MTypeAllowedFast:
		if !pc.ExtBits.IsSupportFast() {
			err = ErrNotSupportFast
		} else {
			pc.Fasts = pc.Fasts.Append(msg.Index)
			if h, ok := handler.(Bep6Handler); ok {
				err = h.AllowedFast(pc, msg.Index)
			} else {
				err = handler.OnMessage(pc, msg)
			}
		}

	// BEP 10 - Extension Protocol
	case MTypeExtended:
		if !pc.ExtBits.IsSupportExtended() {
			err = ErrNotSupportExtended
		} else if h, ok := handler.(Bep10Handler); ok {
			err = pc.handleExtMsg(h, msg)
		} else {
			err = handler.OnMessage(pc, msg)
		}

	// Other
	default:
		err = handler.OnMessage(pc, msg)
	}

	if !pc.notFirstMsg {
		pc.notFirstMsg = true
	}

	return
}

func (pc *PeerConn) handleExtMsg(h Bep10Handler, m Message) (err error) {
	if m.ExtendedID == ExtendedIDHandshake {
		if pc.extHandshake {
			return ErrSecondExtHandshake
		}
		pc.extHandshake = true

		if err = bencode.DecodeBytes(m.ExtendedPayload, &pc.ExtendedHandshakeMsg); err != nil {
			return err
		}

		if pexID, ok := pc.ExtendedHandshakeMsg.M["ut_pex"]; ok {
			pc.PEXID = pexID
		}

		return h.OnExtHandShake(pc)
	}

	if !pc.extHandshake {
		return ErrNoExtHandshake
	}
	return h.OnPayload(pc, m.ExtendedID, m.ExtendedPayload)
}

func (pc *PeerConn) SendPEX(peers []metainfo.Address, dropped []metainfo.Address) error {
	if pc.PEXID == 0 {
		// Peer does not support ut_pex
		return nil
	}
	um := UtPexMsg{}
	um.Added, um.AddedF = toCompactPeers(peers)
	um.Dropped, _ = toCompactPeers(dropped)
	payload, err := bencode.EncodeBytes(um)
	if err != nil {
		return err
	}
	return pc.SendExtMsg(pc.PEXID, payload)
}

func toCompactPeers(peers []metainfo.Address) (added []byte, addedf []byte) {
	for _, p := range peers {
		b, err := p.MarshalBinary()
		if err != nil {
			continue
		}
		if len(b) == 6 {
			added = append(added, b...)
			addedf = append(addedf, 0x00) // seed flag or others can be set if needed
		}
	}
	return
}
