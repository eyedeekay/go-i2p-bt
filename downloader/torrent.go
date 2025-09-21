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

// Package downloader is used to download the torrent or the real file
// from the peer node by the peer wire protocol.
package downloader

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/go-i2p/go-i2p-bt/metainfo"
	pp "github.com/go-i2p/go-i2p-bt/peerprotocol"
)

// BlockSize is the size of a block of the piece.
const BlockSize = 16384 // 16KiB.

// Request is used to send a download request.
type request struct {
	Host     string
	Port     uint16
	PeerID   metainfo.Hash
	InfoHash metainfo.Hash
}

// TorrentResponse represents a torrent info response.
type TorrentResponse struct {
	Host      string        // Which host the torrent is downloaded from
	Port      uint16        // Which port the torrent is downloaded from
	PeerID    metainfo.Hash // ID of the peer where torrent is downloaded from
	InfoHash  metainfo.Hash // The SHA-1 hash of the torrent to be downloaded
	InfoBytes []byte        // The content of the info part in the torrent
}

// TorrentDownloaderConfig is used to configure the TorrentDownloader.
type TorrentDownloaderConfig struct {
	// ID is the id of the downloader peer node.
	//
	// The default is a random id.
	ID metainfo.Hash

	// WorkerNum is the number of the worker downloading the torrent concurrently.
	//
	// The default is 128.
	WorkerNum int

	// DialTimeout is the timeout used by dialing to the peer on TCP.
	DialTimeout time.Duration

	// ErrorLog is used to log the error.
	//
	// The default is log.Printf.
	ErrorLog func(format string, args ...interface{})
}

func (c *TorrentDownloaderConfig) set(conf ...TorrentDownloaderConfig) {
	if len(conf) > 0 {
		*c = conf[0]
	}

	if c.WorkerNum <= 0 {
		c.WorkerNum = 128
	}
	if c.ID.IsZero() {
		c.ID = metainfo.NewRandomHash()
	}
	if c.ErrorLog == nil {
		c.ErrorLog = log.Printf
	}
}

// TorrentDownloader is used to download the torrent file from the peer.
type TorrentDownloader struct {
	conf      TorrentDownloaderConfig
	exit      chan struct{}
	requests  chan request
	responses chan TorrentResponse

	ondht func(string, uint16)
	ebits pp.ExtensionBits
	ehmsg pp.ExtendedHandshakeMsg
}

// NewTorrentDownloader returns a new TorrentDownloader.
//
// If id is ZERO, it is reset to a random id. workerNum is 128 by default.
func NewTorrentDownloader(c ...TorrentDownloaderConfig) *TorrentDownloader {
	var conf TorrentDownloaderConfig
	conf.set(c...)

	d := &TorrentDownloader{
		conf:      conf,
		exit:      make(chan struct{}),
		requests:  make(chan request, conf.WorkerNum),
		responses: make(chan TorrentResponse, 1024),

		ehmsg: pp.ExtendedHandshakeMsg{
			M: map[string]uint8{pp.ExtendedMessageNameMetadata: 1},
		},
	}

	for i := 0; i < conf.WorkerNum; i++ {
		go d.worker()
	}

	d.ebits.Set(pp.ExtensionBitExtended)
	return d
}

// Request submits a download request.
//
// Notice: the remote peer must support the "ut_metadata" extenstion.
// Or downloading fails.
func (d *TorrentDownloader) Request(host string, port uint16, infohash metainfo.Hash) {
	d.requests <- request{Host: host, Port: port, InfoHash: infohash}
}

// Response returns a response channel to get the downloaded torrent info.
func (d *TorrentDownloader) Response() <-chan TorrentResponse {
	return d.responses
}

// Close closes the downloader and releases the underlying resources.
func (d *TorrentDownloader) Close() {
	select {
	case <-d.exit:
	default:
		close(d.exit)
	}
}

// OnDHTNode sets the DHT node callback, which will enable DHT extenstion bit.
//
// In the callback function, you maybe ping it like DHT by UDP.
// If the node responds, you can add the node in DHT routing table.
//
// BEP 5
func (d *TorrentDownloader) OnDHTNode(cb func(host string, port uint16)) {
	d.ondht = cb
	if cb == nil {
		d.ebits.Unset(pp.ExtensionBitDHT)
	} else {
		d.ebits.Set(pp.ExtensionBitDHT)
	}
}

func (d *TorrentDownloader) worker() {
	for {
		select {
		case <-d.exit:
			return
		case r := <-d.requests:
			if err := d.download(r.Host, r.Port, r.PeerID, r.InfoHash); err != nil {
				d.conf.ErrorLog("fail to download the torrent '%s': %s",
					r.InfoHash.HexString(), err)
			}
		}
	}
}

// download performs the torrent metadata download from a peer.
func (d *TorrentDownloader) download(host string, port uint16,
	peerID, infohash metainfo.Hash,
) (err error) {
	conn, err := d.establishConnection(host, port, peerID, infohash)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err = d.performExtendedHandshake(conn); err != nil {
		return err
	}

	return d.processMetadataExchange(conn, host, port, infohash)
}

// establishConnection creates and validates a peer connection.
func (d *TorrentDownloader) establishConnection(host string, port uint16,
	peerID, infohash metainfo.Hash,
) (*pp.PeerConn, error) {
	addr := net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10))
	conn, err := pp.NewPeerConnByDial(addr, d.conf.ID, infohash, d.conf.DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("fail to dial to '%s': %s", addr, err)
	}

	conn.ExtBits = d.ebits
	if err = conn.Handshake(); err != nil {
		conn.Close()
		return nil, err
	}

	if !conn.PeerExtBits.IsSupportExtended() {
		conn.Close()
		return nil, fmt.Errorf("the remote peer '%s' does not support Extended", addr)
	}

	if !peerID.IsZero() && peerID != conn.PeerID {
		conn.Close()
		return nil, fmt.Errorf("inconsistent peer id '%s'", conn.PeerID.HexString())
	}

	return conn, nil
}

// performExtendedHandshake sends the extended handshake message to the peer.
func (d *TorrentDownloader) performExtendedHandshake(conn *pp.PeerConn) error {
	return conn.SendExtHandshakeMsg(d.ehmsg)
}

// processMetadataExchange handles the main message loop for metadata exchange.
func (d *TorrentDownloader) processMetadataExchange(conn *pp.PeerConn, host string, port uint16, infohash metainfo.Hash) error {
	var pieces [][]byte
	var piecesNum int
	var metadataSize int
	var utmetadataID uint8

	return d.runMetadataExchangeLoop(conn, host, port, infohash, &pieces, &piecesNum, &metadataSize, &utmetadataID)
}

// runMetadataExchangeLoop executes the main message processing loop for metadata exchange.
func (d *TorrentDownloader) runMetadataExchangeLoop(conn *pp.PeerConn, host string, port uint16, infohash metainfo.Hash, pieces *[][]byte, piecesNum *int, metadataSize *int, utmetadataID *uint8) error {
	for {
		msg, err := d.readNextMessage(conn)
		if err != nil {
			return err
		}

		if msg.Keepalive {
			continue
		}

		if err = d.handleMessage(msg, host, pieces, piecesNum, metadataSize, utmetadataID, conn, port, infohash); err != nil {
			return err
		}

		if err = d.checkAndFinalizeMetadata(*pieces, host, port, conn.PeerID, infohash); err != nil {
			return err
		}
	}
}

// readNextMessage reads the next message from the connection with exit handling.
func (d *TorrentDownloader) readNextMessage(conn *pp.PeerConn) (pp.Message, error) {
	msg, err := conn.ReadMsg()
	if err != nil {
		return msg, err
	}

	select {
	case <-d.exit:
		return msg, fmt.Errorf("downloader exit requested")
	default:
		return msg, nil
	}
}

// checkAndFinalizeMetadata checks if metadata is complete and finalizes it if ready.
func (d *TorrentDownloader) checkAndFinalizeMetadata(pieces [][]byte, host string, port uint16, peerID, infohash metainfo.Hash) error {
	if pieces != nil && d.isMetadataComplete(pieces) {
		return d.finalizeMetadata(pieces, host, port, peerID, infohash)
	}
	return nil
}

// handleMessage processes individual messages during metadata exchange.
func (d *TorrentDownloader) handleMessage(msg pp.Message, host string, pieces *[][]byte, piecesNum *int, metadataSize *int, utmetadataID *uint8, conn *pp.PeerConn, port uint16, infohash metainfo.Hash) error {
	switch msg.Type {
	case pp.MTypeExtended:
		return d.handleExtendedMessage(msg, pieces, piecesNum, metadataSize, utmetadataID, conn)
	case pp.MTypePort:
		if d.ondht != nil {
			d.ondht(host, msg.Port)
		}
		return nil
	default:
		return nil
	}
}

// handleExtendedMessage processes extended protocol messages.
func (d *TorrentDownloader) handleExtendedMessage(msg pp.Message, pieces *[][]byte, piecesNum *int, metadataSize *int, utmetadataID *uint8, conn *pp.PeerConn) error {
	switch msg.ExtendedID {
	case pp.ExtendedIDHandshake:
		return d.processExtendedHandshake(msg, pieces, piecesNum, metadataSize, utmetadataID, conn)
	case 1:
		return d.processMetadataPiece(msg, *pieces, *piecesNum, *metadataSize)
	default:
		return nil
	}
}

// processExtendedHandshake handles the extended handshake response.
func (d *TorrentDownloader) processExtendedHandshake(msg pp.Message, pieces *[][]byte, piecesNum *int, metadataSize *int, utmetadataID *uint8, conn *pp.PeerConn) error {
	if *utmetadataID > 0 {
		return fmt.Errorf("rehandshake from the peer '%s'", conn.RemoteAddr().String())
	}

	var ehmsg pp.ExtendedHandshakeMsg
	if err := ehmsg.Decode(msg.ExtendedPayload); err != nil {
		return err
	}

	*utmetadataID = ehmsg.M[pp.ExtendedMessageNameMetadata]
	if *utmetadataID == 0 {
		return errors.New(`the peer does not support "ut_metadata"`)
	}

	*metadataSize = ehmsg.MetadataSize
	*piecesNum = *metadataSize / BlockSize
	if *metadataSize%BlockSize != 0 {
		(*piecesNum)++
	}

	*pieces = make([][]byte, *piecesNum)
	go d.requestPieces(conn, *utmetadataID, *piecesNum)
	return nil
}

// processMetadataPiece handles incoming metadata piece data.
func (d *TorrentDownloader) processMetadataPiece(msg pp.Message, pieces [][]byte, piecesNum int, metadataSize int) error {
	if pieces == nil {
		return nil
	}

	var utmsg pp.UtMetadataExtendedMsg
	if err := utmsg.DecodeFromPayload(msg.ExtendedPayload); err != nil {
		return err
	}

	if utmsg.MsgType != pp.UtMetadataExtendedMsgTypeData {
		return nil
	}

	if err := d.validatePieceData(utmsg, piecesNum, metadataSize); err != nil {
		return err
	}

	pieces[utmsg.Piece] = utmsg.Data
	return nil
}

// validatePieceData validates the received metadata piece data.
func (d *TorrentDownloader) validatePieceData(utmsg pp.UtMetadataExtendedMsg, piecesNum int, metadataSize int) error {
	pieceLen := len(utmsg.Data)
	if utmsg.Piece == piecesNum-1 {
		expectedLen := metadataSize % BlockSize
		if expectedLen == 0 {
			expectedLen = BlockSize
		}
		if pieceLen != expectedLen {
			return fmt.Errorf("invalid final piece length: expected %d, got %d", expectedLen, pieceLen)
		}
	} else if pieceLen != BlockSize {
		return fmt.Errorf("invalid piece length: expected %d, got %d", BlockSize, pieceLen)
	}
	return nil
}

// isMetadataComplete checks if all metadata pieces have been received.
func (d *TorrentDownloader) isMetadataComplete(pieces [][]byte) bool {
	for _, piece := range pieces {
		if len(piece) == 0 {
			return false
		}
	}
	return true
}

// finalizeMetadata assembles the complete metadata and sends the response.
func (d *TorrentDownloader) finalizeMetadata(pieces [][]byte, host string, port uint16, peerID, infohash metainfo.Hash) error {
	metadataInfo := bytes.Join(pieces, nil)
	if infohash == metainfo.Hash(sha1.Sum(metadataInfo)) {
		d.responses <- TorrentResponse{
			Host:      host,
			Port:      port,
			PeerID:    peerID,
			InfoHash:  infohash,
			InfoBytes: metadataInfo,
		}
	}
	return nil
}

func (d *TorrentDownloader) requestPieces(conn *pp.PeerConn, utMetadataID uint8, piecesNum int) {
	for i := 0; i < piecesNum; i++ {
		payload, err := pp.UtMetadataExtendedMsg{
			MsgType: pp.UtMetadataExtendedMsgTypeRequest,
			Piece:   i,
		}.EncodeToBytes()
		if err != nil {
			panic(err)
		}

		msg := pp.Message{
			Type:            pp.MTypeExtended,
			ExtendedID:      utMetadataID,
			ExtendedPayload: payload,
		}

		if err = conn.WriteMsg(msg); err != nil {
			d.conf.ErrorLog("fail to send the ut_metadata request: %s", err)
			return
		}
	}
}
