package peerprotocol

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-i2p/go-i2p-bt/bencode"
	"github.com/go-i2p/go-i2p-bt/metainfo"
)

// DebugPrintHex prints the hex representation of data for debugging.
func DebugPrintHex(prefix string, data []byte) {
	if len(data) == 0 {
		log.Printf("%s: [empty data]", prefix)
		return
	}
	log.Printf("%s: len=%d, hex=%s", prefix, len(data), hex.EncodeToString(data))
}

// testPEXHandler is used by the OnPayload callback to record added/dropped peers.
type testPEXHandler struct {
	NoopHandler
	added   []metainfo.Address
	dropped []metainfo.Address
}

func (h *testPEXHandler) OnExtHandShake(pc *PeerConn) error {
	return nil
}

func (h *testPEXHandler) OnPayload(pc *PeerConn, extid uint8, payload []byte) error {
	if extid == pc.PEXID && pc.PEXID != 0 {
		um, err := DecodePexMsg(payload)
		if err != nil {
			return err
		}
		newPeers := parseCompactPeers(um.Added)
		h.added = append(h.added, newPeers...)
		remPeers := parseCompactPeers(um.Dropped)
		h.dropped = append(h.dropped, remPeers...)
	}
	return nil
}

// doTestHandshakeOrdered now uses concurrency to avoid deadlock.
func doTestHandshakeOrdered(pc1, pc2 *PeerConn, t *testing.T) error {
	t.Logf("doTestHandshakeOrdered: start")

	m1 := HandshakeMsg{ExtensionBits: pc1.ExtBits, PeerID: pc1.ID, InfoHash: pc1.InfoHash}
	buf1 := new(bytes.Buffer)
	buf1.WriteString(ProtocolHeader)
	buf1.Write(m1.ExtensionBits[:])
	buf1.Write(m1.InfoHash[:])
	buf1.Write(m1.PeerID[:])

	var wg sync.WaitGroup
	wg.Add(1)
	var err2 error
	go func() {
		defer wg.Done()
		r2 := make([]byte, 68)
		t.Logf("pc2 reading handshake...")
		if _, err := io.ReadFull(pc2.Conn, r2); err != nil {
			err2 = fmt.Errorf("pc2 read handshake: %v", err)
			return
		}
		if string(r2[:20]) != ProtocolHeader {
			err2 = fmt.Errorf("pc2 invalid protocol header")
			return
		}
		copy(pc2.PeerExtBits[:], r2[20:28])
		copy(pc2.InfoHash[:], r2[28:48])
		copy(pc2.PeerID[:], r2[48:68])
		t.Logf("pc2 read handshake from pc1")
	}()

	t.Logf("pc1 writing handshake (%d bytes)", buf1.Len())
	if _, err := pc1.Conn.Write(buf1.Bytes()); err != nil {
		return fmt.Errorf("pc1 write handshake: %v", err)
	}

	wg.Wait()
	if err2 != nil {
		return err2
	}

	m2 := HandshakeMsg{ExtensionBits: pc2.ExtBits, PeerID: pc2.ID, InfoHash: pc2.InfoHash}
	buf2 := new(bytes.Buffer)
	buf2.WriteString(ProtocolHeader)
	buf2.Write(m2.ExtensionBits[:])
	buf2.Write(m2.InfoHash[:])
	buf2.Write(m2.PeerID[:])

	wg.Add(1)
	var err1 error
	go func() {
		defer wg.Done()
		r1 := make([]byte, 68)
		t.Logf("pc1 reading handshake from pc2...")
		if _, err := io.ReadFull(pc1.Conn, r1); err != nil {
			err1 = fmt.Errorf("pc1 read handshake: %v", err)
			return
		}
		if string(r1[:20]) != ProtocolHeader {
			err1 = fmt.Errorf("pc1 invalid protocol header")
			return
		}
		copy(pc1.PeerExtBits[:], r1[20:28])
		copy(pc1.InfoHash[:], r1[28:48])
		copy(pc1.PeerID[:], r1[48:68])
		t.Logf("pc1 read handshake from pc2 done")
	}()

	t.Logf("pc2 writing handshake (%d bytes)", buf2.Len())
	if _, err := pc2.Conn.Write(buf2.Bytes()); err != nil {
		return fmt.Errorf("pc2 write handshake: %v", err)
	}

	wg.Wait()
	if err1 != nil {
		return err1
	}

	return nil
}

// doTestExtendedHandshakeOrdered now also uses concurrency to avoid deadlock.
func doTestExtendedHandshakeOrdered(pc1, pc2 *PeerConn, e1, e2 ExtendedHandshakeMsg, t *testing.T) error {
	// Print the original structs
	log.Printf("Encoding e1: %+v", e1)
	log.Printf("Encoding e2: %+v", e2)

	b1, err := e1.Encode()
	if err != nil {
		return fmt.Errorf("encode e1: %v", err)
	}
	DebugPrintHex("Encoded e1", b1)

	b2, err := e2.Encode()
	if err != nil {
		return fmt.Errorf("encode e2: %v", err)
	}
	DebugPrintHex("Encoded e2", b2)

	t.Logf("doTestExtendedHandshakeOrdered: e1 len=%d, e2 len=%d", len(b1), len(b2))

	// Attempt to decode immediately to verify encoding
	var testDec1, testDec2 ExtendedHandshakeMsg
	if err := bencode.DecodeBytes(b1, &testDec1); err != nil {
		log.Printf("Decoding e1 after encoding failed: %v", err)
	} else {
		log.Printf("e1 successfully decoded right after encoding: %+v", testDec1)
	}

	if err := bencode.DecodeBytes(b2, &testDec2); err != nil {
		log.Printf("Decoding e2 after encoding failed: %v", err)
	} else {
		log.Printf("e2 successfully decoded right after encoding: %+v", testDec2)
	}

	msg1 := Message{Type: MTypeExtended, ExtendedID: ExtendedIDHandshake, ExtendedPayload: b1}

	var wg sync.WaitGroup
	wg.Add(1)
	var err2 error
	go func() {
		defer wg.Done()
		t.Logf("pc2 reading extended handshake...")
		m, err := pc2.ReadMsg()
		if err != nil {
			err2 = fmt.Errorf("pc2 read ext handshake: %v", err)
			return
		}
		if m.Type != MTypeExtended || m.ExtendedID != ExtendedIDHandshake {
			err2 = fmt.Errorf("pc2 expected extended handshake, got something else")
			return
		}
		DebugPrintHex("pc2 got extended handshake payload", m.ExtendedPayload)
		if err := bencode.DecodeBytes(m.ExtendedPayload, &pc2.ExtendedHandshakeMsg); err != nil {
			err2 = fmt.Errorf("pc2 decode ext handshake: %v", err)
			return
		}
		t.Logf("pc2 got extended handshake from pc1")
		pc2.extHandshake = true
	}()

	t.Logf("pc1 writing extended handshake...")
	if err := pc1.WriteMsg(msg1); err != nil {
		return fmt.Errorf("pc1 write ext handshake: %v", err)
	}
	t.Logf("pc1 wrote extended handshake")
	wg.Wait()
	if err2 != nil {
		return err2
	}

	msg2 := Message{Type: MTypeExtended, ExtendedID: ExtendedIDHandshake, ExtendedPayload: b2}

	wg.Add(1)
	var err1 error
	go func() {
		defer wg.Done()
		t.Logf("pc1 reading extended handshake...")
		m, err := pc1.ReadMsg()
		if err != nil {
			err1 = fmt.Errorf("pc1 read ext handshake: %v", err)
			return
		}
		if m.Type != MTypeExtended || m.ExtendedID != ExtendedIDHandshake {
			err1 = fmt.Errorf("pc1 expected extended handshake, got something else")
			return
		}
		DebugPrintHex("pc1 got extended handshake payload", m.ExtendedPayload)
		if err := bencode.DecodeBytes(m.ExtendedPayload, &pc1.ExtendedHandshakeMsg); err != nil {
			err1 = fmt.Errorf("pc1 decode ext handshake: %v", err)
			return
		}
		t.Logf("pc1 got extended handshake from pc2")
		pc1.extHandshake = true
	}()

	t.Logf("pc2 writing extended handshake...")
	if err := pc2.WriteMsg(msg2); err != nil {
		return fmt.Errorf("pc2 write ext handshake: %v", err)
	}
	t.Logf("pc2 wrote extended handshake")
	wg.Wait()
	if err1 != nil {
		return err1
	}

	return nil
}

// TestPEX tests the ut_pex functionality.
func TestPEX(t *testing.T) {
	t.Logf("Starting TestPEX")
	serverConn, clientConn := net.Pipe()

	localID := metainfo.NewRandomHash()
	remoteID := metainfo.NewRandomHash()
	infoHash := metainfo.NewRandomHash()

	t.Logf("LocalID=%s RemoteID=%s InfoHash=%s", localID.HexString(), remoteID.HexString(), infoHash.HexString())

	localBits := ExtensionBits{}
	localBits.Set(ExtensionBitExtended)
	remoteBits := ExtensionBits{}
	remoteBits.Set(ExtensionBitExtended)

	localPC := NewPeerConn(serverConn, localID, infoHash)
	localPC.ExtBits = localBits
	localPC.Timeout = 3 * time.Second
	localPC.MaxLength = 256 * 1024
	t.Logf("localPC created")

	remotePC := NewPeerConn(clientConn, remoteID, infoHash)
	remotePC.ExtBits = remoteBits
	remotePC.Timeout = 3 * time.Second
	remotePC.MaxLength = 256 * 1024
	t.Logf("remotePC created")

	pexHandler := &testPEXHandler{}
	t.Logf("testPEXHandler created")

	t.Logf("Performing handshake...")
	if err := doTestHandshakeOrdered(localPC, remotePC, t); err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	t.Logf("Handshake done")

	// Ensure M has some non-empty fields and no unsupported types
	localEHMsg := ExtendedHandshakeMsg{
		M: map[string]uint8{
			"ut_metadata": 1,
			"ut_pex":      2,
			"dummy":       42,
		},
		V:    "nonempty",
		Reqq: 1,
		Port: 6881,
	}
	remoteEHMsg := ExtendedHandshakeMsg{
		M: map[string]uint8{
			"ut_metadata": 1,
			"ut_pex":      2,
			"dummy":       42,
		},
		V:    "nonempty",
		Reqq: 1,
		Port: 6881,
	}

	t.Logf("Performing extended handshake...")
	if err := doTestExtendedHandshakeOrdered(localPC, remotePC, localEHMsg, remoteEHMsg, t); err != nil {
		t.Fatalf("extended handshake failed: %v", err)
	}
	t.Logf("Extended handshake done")

	if localPC.PEXID == 0 || remotePC.PEXID == 0 {
		t.Fatalf("ut_pex not set properly: localPEXID=%d remotePEXID=%d", localPC.PEXID, remotePC.PEXID)
	}
	t.Logf("PEXID local=%d remote=%d", localPC.PEXID, remotePC.PEXID)

	done := make(chan struct{})
	t.Logf("Starting remote read goroutine")
	go func() {
		defer close(done)
		for {
			t.Logf("GOROUTINE: remotePC about to ReadMsg()...")
			msg, err := remotePC.ReadMsg()
			if err != nil {
				t.Logf("GOROUTINE: remotePC.ReadMsg() returned err=%v, exiting goroutine", err)
				return
			}
			t.Logf("GOROUTINE: remotePC.HandleMessage(): msg type=%v", msg.Type)
			if err := remotePC.HandleMessage(msg, pexHandler); err != nil {
				t.Logf("GOROUTINE: remotePC.HandleMessage returned err=%v, exiting goroutine", err)
				return
			}
		}
	}()

	t.Logf("Sending PEX message from local to remote")
	addedPeers := []metainfo.Address{
		metainfo.NewAddress(net.ParseIP("1.2.3.4"), 6881),
		metainfo.NewAddress(net.ParseIP("5.6.7.8"), 6882),
	}
	droppedPeers := []metainfo.Address{
		metainfo.NewAddress(net.ParseIP("9.10.11.12"), 6883),
	}

	t.Logf("localPC SendPEX...")
	if err := localPC.SendPEX(addedPeers, droppedPeers); err != nil {
		t.Fatalf("SendPEX failed: %v", err)
	}
	t.Logf("PEX message sent. Waiting for remote to process...")

	time.Sleep(300 * time.Millisecond)
	t.Logf("Checking results on remote side")

	if len(pexHandler.added) != len(addedPeers) {
		t.Fatalf("expected %d added peers, got %d", len(addedPeers), len(pexHandler.added))
	}
	for i, addr := range addedPeers {
		if pexHandler.added[i].String() != addr.String() {
			t.Fatalf("added peer mismatch: got %s, want %s", pexHandler.added[i].String(), addr.String())
		}
	}
	if len(pexHandler.dropped) != len(droppedPeers) {
		t.Fatalf("expected %d dropped peers, got %d", len(droppedPeers), len(pexHandler.dropped))
	}
	for i, addr := range droppedPeers {
		if pexHandler.dropped[i].String() != addr.String() {
			t.Fatalf("dropped peer mismatch: got %s, want %s", pexHandler.dropped[i].String(), addr.String())
		}
	}

	t.Logf("PEX message verification done. Closing connections...")
	serverConn.Close()
	clientConn.Close()
	t.Logf("Connections closed, waiting for goroutine to exit...")

	select {
	case <-done:
		t.Logf("Goroutine exited cleanly")
	case <-time.After(1 * time.Second):
		t.Fatalf("reading goroutine did not exit, still blocked?")
	}
}
