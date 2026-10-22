package rtc

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/base"
)

func TestG711UnpackerEmitsFrame(t *testing.T) {
	cases := []struct {
		name     string
		mime     string
		pt       uint8
		wantType base.AvPacketPt
	}{
		{name: "pcma", mime: webrtc.MimeTypePCMA, pt: 8, wantType: base.AvPacketPtG711A},
		{name: "pcmu", mime: webrtc.MimeTypePCMU, pt: 0, wantType: base.AvPacketPtG711U},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan base.AvPacket, 1)
			unpacker := NewUnPacker(tt.mime, 8000, ch)
			if unpacker.dec == nil {
				t.Fatal("decoder is nil")
			}

			err := unpacker.UnPack(&rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    tt.pt,
					SequenceNumber: 1,
					Timestamp:      0,
				},
				Payload: []byte{0xD5, 0x12, 0x34, 0x56},
			})
			if err != nil {
				t.Fatal(err)
			}

			select {
			case got := <-ch:
				if got.PayloadType != tt.wantType {
					t.Fatalf("payload type = %v", got.PayloadType)
				}
				if len(got.Payload) != 4 {
					t.Fatalf("payload = %v", got.Payload)
				}
			case <-time.After(time.Second):
				t.Fatal("no audio frame")
			}
		})
	}
}
