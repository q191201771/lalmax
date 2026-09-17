package rtppush

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/q191201771/lal/pkg/aac"
	"github.com/q191201771/lal/pkg/avc"
	lalbase "github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/rtprtcp"
	"github.com/q191201771/lalmax/gb28181/mpegps"
)

func TestLowerPushSessionMultiPESFrameMarker(t *testing.T) {
	session := NewLowerPushSession()
	session.SetSsrc(0x11223344)
	sid := session.psMuxer.AddStream(mpegps.PsStreamH264)
	var packets []rtprtcp.RtpPacket
	var psData []byte
	session.psMuxer.OnPacket = func(pkg []byte, pts uint64) {
		psData = append(psData, pkg...)
		packets = append(packets, session.packRtp(pkg, uint32(pts))...)
	}

	// A large IDR needs four PES packets; the next small frame must still
	// get its own marker and timestamp without waiting for another frame.
	seq := uint16(1)
	for i, size := range []int{200000, 32} {
		packets = nil
		psData = nil
		frame := bytes.Repeat([]byte{0x55}, size)
		copy(frame, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
		pts := uint64(1000 + i*40)
		if err := session.psMuxer.Write(sid, frame, pts, pts); err != nil {
			t.Fatal(err)
		}
		if len(packets) == 0 {
			t.Fatal("no RTP packets")
		}
		var reassembled []byte
		for j, pkt := range packets {
			parsed, err := rtprtcp.ParseRtpPacket(pkt.Raw)
			if err != nil {
				t.Fatal(err)
			}
			wantMark := uint8(0)
			if j == len(packets)-1 {
				wantMark = 1
			}
			if parsed.Header.Mark != wantMark {
				t.Fatalf("frame %d, packet %d/%d: marker = %d, want %d", i, j+1, len(packets), parsed.Header.Mark, wantMark)
			}
			if parsed.Header.Seq != seq || parsed.Header.Timestamp != uint32(pts*90) || parsed.Header.Ssrc != 0x11223344 {
				t.Fatalf("unexpected RTP header: %+v", parsed.Header)
			}
			seq++
			if len(parsed.Body()) > lowerPushRtpPacketMax {
				t.Fatalf("RTP payload too large: %d", len(parsed.Body()))
			}
			reassembled = append(reassembled, parsed.Body()...)
		}
		if !bytes.Equal(reassembled, psData) {
			t.Fatal("RTP payload does not preserve complete PS data")
		}
	}
}

func TestLowerPushSessionUDPWriteRtpPacket(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer ln.Close()

	serverAddr := ln.LocalAddr().(*net.UDPAddr)
	got := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 1500)
		_ = ln.SetDeadline(time.Now().Add(3 * time.Second))
		n, _, err := ln.ReadFrom(buf)
		if err != nil {
			errCh <- err
			return
		}
		got <- append([]byte(nil), buf[:n]...)
	}()

	session := NewLowerPushSession()
	session.SetPeerIP(serverAddr.IP.String())
	session.SetPeerPort(serverAddr.Port)
	if err := session.Start("udp"); err != nil {
		t.Fatalf("start udp: %v", err)
	}
	defer session.Dispose()

	pkt := makeTestRtpPacket([]byte{0x11, 0x22, 0x33, 0x44})
	if err := session.WriteRtpPacket(pkt); err != nil {
		t.Fatalf("write udp rtp: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("udp read failed: %v", err)
	case b := <-got:
		if string(b) != string(pkt.Raw) {
			t.Fatalf("udp payload mismatch, got=%v want=%v", b, pkt.Raw)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("udp read timeout")
	}
}

func TestLowerPushSessionTCPWriteRtpPsPacket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	got := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 6)
		if _, err := io.ReadFull(conn, buf); err != nil {
			errCh <- err
			return
		}
		got <- buf
	}()

	addr := ln.Addr().(*net.TCPAddr)
	session := NewLowerPushSession()
	session.SetPeerIP(addr.IP.String())
	session.SetPeerPort(addr.Port)
	if err := session.Start("tcp"); err != nil {
		t.Fatalf("start tcp: %v", err)
	}
	defer session.Dispose()

	rawRTP := []byte{0x80, 0x60, 0x00, 0x01}
	if err := session.WriteRtpPsPacket(rawRTP); err != nil {
		t.Fatalf("write tcp rtp/ps: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("tcp read failed: %v", err)
	case b := <-got:
		want := []byte{0x00, 0x04, 0x80, 0x60, 0x00, 0x01}
		if string(b) != string(want) {
			t.Fatalf("tcp payload mismatch, got=%v want=%v", b, want)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("tcp read timeout")
	}
}

func TestLowerPushSessionWriteBeforeStart(t *testing.T) {
	session := NewLowerPushSession()
	err := session.WriteRtpPacket(makeTestRtpPacket([]byte{0x01}))
	if err != lalbase.ErrSessionNotStarted {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLowerPushSessionOnMsgVideoUDP(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer ln.Close()

	serverAddr := ln.LocalAddr().(*net.UDPAddr)
	got := make(chan []byte, 2)
	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			buf := make([]byte, 2048)
			_ = ln.SetDeadline(time.Now().Add(3 * time.Second))
			n, _, err := ln.ReadFrom(buf)
			if err != nil {
				errCh <- err
				return
			}
			got <- append([]byte(nil), buf[:n]...)
		}
	}()

	session := NewLowerPushSession()
	session.SetPeerIP(serverAddr.IP.String())
	session.SetPeerPort(serverAddr.Port)
	session.SetSsrc(0x11223344)
	if err := session.Start("udp"); err != nil {
		t.Fatalf("start udp: %v", err)
	}
	defer session.Dispose()

	session.OnMsg(makeAvcSeqHeaderMsg())
	session.OnMsg(makeAacSeqHeaderMsg())
	session.OnMsg(makeAvcKeyFrameMsg())
	session.OnMsg(makeAacRawMsg())

	var pkts []rtprtcp.RtpPacket
	deadline := time.After(4 * time.Second)
	for len(pkts) < 2 {
		select {
		case err := <-errCh:
			t.Fatalf("udp read failed: %v", err)
		case b := <-got:
			pkt, err := rtprtcp.ParseRtpPacket(b)
			if err != nil {
				t.Fatalf("parse rtp packet failed: %v", err)
			}
			pkts = append(pkts, pkt)
		case <-deadline:
			t.Fatal("udp onmsg timeout")
		}
	}

	foundPS := false
	for _, pkt := range pkts {
		if pkt.Header.Ssrc != 0x11223344 {
			t.Fatalf("ssrc mismatch. got=%d", pkt.Header.Ssrc)
		}
		if pkt.Header.PacketType != uint8(lalbase.AvPacketPtAvc) {
			t.Fatalf("payload type mismatch. got=%d", pkt.Header.PacketType)
		}
		body := pkt.Body()
		if len(body) >= 4 && body[0] == 0x00 && body[1] == 0x00 && body[2] == 0x01 && body[3] == 0xBA {
			foundPS = true
		}
	}
	if !foundPS {
		t.Fatalf("expected ps pack header in udp packets")
	}
}

func TestLowerPushSessionOnMsgAudioTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()

	got := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		sizeBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, sizeBuf); err != nil {
			errCh <- err
			return
		}
		size := int(sizeBuf[0])<<8 | int(sizeBuf[1])
		payload := make([]byte, size)
		if _, err := io.ReadFull(conn, payload); err != nil {
			errCh <- err
			return
		}
		got <- append(sizeBuf, payload...)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	session := NewLowerPushSession()
	session.SetPeerIP(addr.IP.String())
	session.SetPeerPort(addr.Port)
	session.SetSsrc(0x55667788)
	if err := session.Start("tcp"); err != nil {
		t.Fatalf("start tcp: %v", err)
	}
	defer session.Dispose()

	session.OnMsg(makeG711AMsg())

	select {
	case err := <-errCh:
		t.Fatalf("tcp read failed: %v", err)
	case b := <-got:
		if len(b) < 14 {
			t.Fatalf("tcp packet too short: %d", len(b))
		}
		size := int(b[0])<<8 | int(b[1])
		if size != len(b)-2 {
			t.Fatalf("tcp length mismatch. prefix=%d actual=%d", size, len(b)-2)
		}
		pkt, err := rtprtcp.ParseRtpPacket(b[2:])
		if err != nil {
			t.Fatalf("parse tcp rtp failed: %v", err)
		}
		if pkt.Header.Ssrc != 0x55667788 {
			t.Fatalf("ssrc mismatch. got=%d", pkt.Header.Ssrc)
		}
		body := pkt.Body()
		if len(body) < 4 || body[0] != 0x00 || body[1] != 0x00 || body[2] != 0x01 || body[3] != 0xBA {
			t.Fatalf("expected ps pack header, body prefix=%v", body[:min(4, len(body))])
		}
	case <-time.After(4 * time.Second):
		t.Fatal("tcp onmsg timeout")
	}
}

func makeTestRtpPacket(payload []byte) rtprtcp.RtpPacket {
	h := rtprtcp.MakeDefaultRtpHeader()
	h.PacketType = uint8(lalbase.AvPacketPtAvc)
	h.Seq = 1
	h.Timestamp = 90000
	h.Ssrc = 1234
	return rtprtcp.MakeRtpPacket(h, payload)
}

func makeAvcSeqHeaderMsg() lalbase.RtmpMsg {
	payload := []byte{
		0x17, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x64, 0x00, 0x20, 0xFF,
		0xE1, 0x00, 0x19,
		0x67, 0x64, 0x00, 0x20, 0xAC, 0xD9, 0x40, 0xC0, 0x29, 0xB0, 0x11, 0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x03, 0x00, 0x32, 0x0F, 0x18, 0x31, 0x96,
		0x01, 0x00, 0x05,
		0x68, 0xEB, 0xEC, 0xB2, 0x2C,
	}
	return lalbase.RtmpMsg{
		Header: lalbase.RtmpHeader{
			MsgTypeId:    lalbase.RtmpTypeIdVideo,
			MsgLen:       uint32(len(payload)),
			TimestampAbs: 0,
		},
		Payload: payload,
	}
}

func makeAvcKeyFrameMsg() lalbase.RtmpMsg {
	idr := []byte{0x65, 0x88, 0x84, 0x21, 0xA0}
	payload := make([]byte, 5+4+len(idr))
	payload[0] = lalbase.RtmpAvcKeyFrame
	payload[1] = lalbase.RtmpAvcPacketTypeNalu
	payload[2] = 0
	payload[3] = 0
	payload[4] = 0
	payload[5] = 0
	payload[6] = 0
	payload[7] = 0
	payload[8] = byte(len(idr))
	copy(payload[9:], idr)
	return lalbase.RtmpMsg{
		Header: lalbase.RtmpHeader{
			MsgTypeId:    lalbase.RtmpTypeIdVideo,
			MsgLen:       uint32(len(payload)),
			TimestampAbs: 40,
		},
		Payload: payload,
	}
}

func makeAacSeqHeaderMsg() lalbase.RtmpMsg {
	payload := []byte{0xAF, 0x00, 0x11, 0x90}
	return lalbase.RtmpMsg{
		Header: lalbase.RtmpHeader{
			MsgTypeId:    lalbase.RtmpTypeIdAudio,
			MsgLen:       uint32(len(payload)),
			TimestampAbs: 0,
		},
		Payload: payload,
	}
}

func makeAacRawMsg() lalbase.RtmpMsg {
	raw := []byte{0x21, 0x2B, 0x94, 0xA5, 0xB6, 0x0A, 0xE1, 0x63}
	payload := append([]byte{0xAF, 0x01}, raw...)
	return lalbase.RtmpMsg{
		Header: lalbase.RtmpHeader{
			MsgTypeId:    lalbase.RtmpTypeIdAudio,
			MsgLen:       uint32(len(payload)),
			TimestampAbs: 40,
		},
		Payload: payload,
	}
}

func makeG711AMsg() lalbase.RtmpMsg {
	payload := []byte{lalbase.RtmpSoundFormatG711A << 4, 0xD5, 0x5A, 0x11, 0x22}
	return lalbase.RtmpMsg{
		Header: lalbase.RtmpHeader{
			MsgTypeId:    lalbase.RtmpTypeIdAudio,
			MsgLen:       uint32(len(payload)),
			TimestampAbs: 20,
		},
		Payload: payload,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFixturesAreValid(t *testing.T) {
	if _, _, err := avc.ParseSpsPpsFromSeqHeader(makeAvcSeqHeaderMsg().Payload); err != nil {
		t.Fatalf("invalid avc fixture: %v", err)
	}
	if _, err := aac.NewAscContext(makeAacSeqHeaderMsg().Payload[2:]); err != nil {
		t.Fatalf("invalid aac fixture: %v", err)
	}
}
