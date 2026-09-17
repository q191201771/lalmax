package mpegps

import (
	"bytes"
	"fmt"
	"testing"
)

func TestPsMuxerWriteOutputsCompleteFrame(t *testing.T) {
	for _, codec := range []struct {
		name       string
		streamType PsStreamType
		naluHeader []byte
		aud        []byte
	}{
		{"H264", PsStreamH264, []byte{0x65}, H264AudNalu},
		{"H265", PsStreamH265, []byte{0x26, 0x01}, H265AudNalu},
	} {
		// The first PES also contains the AUD and 13 bytes of optional headers.
		firstPayloadMax := 0xFFFF - 13 - len(codec.aud)
		for _, size := range []int{32, firstPayloadMax - 1, firstPayloadMax, firstPayloadMax + 1, 200000} {
			t.Run(fmt.Sprintf("%s/%d", codec.name, size), func(t *testing.T) {
				muxer := NewPsMuxer()
				sid := muxer.AddStream(codec.streamType)
				frame := bytes.Repeat([]byte{0x55}, size)
				copy(frame, []byte{0x00, 0x00, 0x00, 0x01})
				copy(frame[4:], codec.naluHeader)
				var packets [][]byte
				muxer.OnPacket = func(pkg []byte, pts uint64) {
					if pts != 90000 {
						t.Errorf("PTS = %d, want 90000", pts)
					}
					packets = append(packets, append([]byte(nil), pkg...))
				}
				if err := muxer.Write(sid, frame, 1000, 1000); err != nil {
					t.Fatal(err)
				}
				if len(packets) != 1 {
					t.Fatalf("callbacks per frame = %d, want 1", len(packets))
				}

				var payload []byte
				pesCount := 0
				demuxer := NewPsDemuxer()
				demuxer.OnPacket = func(pkg Display, err error) {
					if err != nil {
						t.Fatalf("decode PS: %v", err)
					}
					if pes, ok := pkg.(*PesPacket); ok {
						pesCount++
						if pes.StreamId != sid || pes.Pts != 90000 || pes.Dts != 90000 {
							t.Fatalf("unexpected PES stream/timestamps: %+v", pes)
						}
						payload = append(payload, pes.PesPayload...)
					}
				}
				if err := demuxer.Input(packets[0]); err != nil {
					t.Fatalf("demux frame: %v", err)
				}
				wantPES := 1
				if size > firstPayloadMax {
					wantPES += (size - firstPayloadMax + (0xFFFF - 13) - 1) / (0xFFFF - 13)
				}
				if pesCount != wantPES {
					t.Fatalf("PES count = %d, want %d", pesCount, wantPES)
				}
				wantPayload := append(append([]byte(nil), codec.aud...), frame...)
				if !bytes.Equal(payload, wantPayload) {
					t.Fatalf("PES payload mismatch: got %d bytes, want %d", len(payload), len(wantPayload))
				}
			})
		}
	}
}
