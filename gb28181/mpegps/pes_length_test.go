package mpegps

import (
	"bytes"
	"testing"
)

func TestPesPacketLengthZeroReturnsPayload(t *testing.T) {
	payload := []byte{0x11, 0x00, 0x00, 0x01, 0x65, 0x22, 0x33}
	raw := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80, 0x80, 0x05,
		0x21, 0x00, 0x01, 0x00, 0x01,
	}
	raw = append(raw, payload...)
	raw = append(raw, 0x00, 0x00, 0x01, 0xBA)

	pkg := NewPesPacket()
	bs := NewBitStream(raw)
	err := pkg.Decode(bs)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(pkg.PesPayload, payload) {
		t.Fatalf("payload = %x", pkg.PesPayload)
	}
	if !bytes.Equal(bs.RemainData(), []byte{0x00, 0x00, 0x01, 0xBA}) {
		t.Fatalf("remaining data = %x", bs.RemainData())
	}
}

func TestPesPacketLengthZeroNeedsNextPacket(t *testing.T) {
	raw := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80, 0x80, 0x05,
		0x21, 0x00, 0x01, 0x00, 0x01,
		0x11, 0x22, 0x33,
	}

	err := NewPesPacket().Decode(NewBitStream(raw))
	mpegErr, ok := err.(Error)
	if !ok || !mpegErr.NeedMore() {
		t.Fatalf("error = %v", err)
	}
}

func TestMpeg1PesPacketLengthZeroReturnsPayload(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}
	raw := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x0F,
	}
	raw = append(raw, payload...)
	raw = append(raw, 0x00, 0x00, 0x01, 0xBA)

	pkg := NewPesPacket()
	bs := NewBitStream(raw)
	if err := pkg.DecodeMpeg1(bs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(pkg.PesPayload, payload) {
		t.Fatalf("payload = %x", pkg.PesPayload)
	}
}

func TestPesPacketExplicitLengthReturnsPayload(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}
	raw := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x0B,
		0x80, 0x80, 0x05,
		0x21, 0x00, 0x01, 0x00, 0x01,
	}
	raw = append(raw, payload...)
	raw = append(raw, 0x00, 0x00, 0x01, 0xBA)

	pkg := NewPesPacket()
	err := pkg.Decode(NewBitStream(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(pkg.PesPayload, payload) {
		t.Fatalf("payload = %x", pkg.PesPayload)
	}
}

func TestPsDemuxerShortPesHeaderDoesNotPanic(t *testing.T) {
	demuxer := NewPsDemuxer()
	data := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x05, 0x80, 0x80, 0x02, 0x21, 0x00}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Input panicked: %v", recovered)
		}
	}()
	if err := demuxer.Input(data); err == nil {
		t.Fatal("truncated PES header was accepted")
	}
}

func TestPsDemuxerLengthZeroAcrossInputs(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}
	pes := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80, 0x80, 0x05,
		0x21, 0x00, 0x01, 0x00, 0x01,
	}
	pes = append(pes, payload...)
	pack := []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0xFF}

	demuxer := NewPsDemuxer()
	var got []byte
	demuxer.OnPacket = func(display Display, err error) {
		if packet, ok := display.(*PesPacket); ok && err == nil {
			got = append([]byte(nil), packet.PesPayload...)
		}
	}
	if err := demuxer.Input(pes); err == nil {
		t.Fatal("zero-length PES without boundary was accepted")
	}
	if err := demuxer.Input(pack); err != nil {
		t.Fatalf("second input: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x", got)
	}
}

func TestPsDemuxerClearsCacheAfterParserError(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x08,
		0x80, 0x80, 0x00,
		0x21, 0x00, 0x01, 0x00, 0x01,
	}

	demuxer := NewPsDemuxer()
	if err := demuxer.Input(data[:8]); err == nil {
		t.Fatal("partial input was accepted")
	}
	if len(demuxer.cache) == 0 {
		t.Fatal("partial input was not cached")
	}
	if err := demuxer.Input(data[8:]); err == nil {
		t.Fatal("invalid PES header was accepted")
	}
	if len(demuxer.cache) != 0 {
		t.Fatalf("cache length = %d", len(demuxer.cache))
	}
}
