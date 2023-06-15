package rtc

import (
	"errors"
	"fmt"
	"time"

	"github.com/aler9/gortsplib/v2/pkg/rtpreorderer"
	"github.com/bluenviron/gortsplib/v3/pkg/formats"
	"github.com/bluenviron/gortsplib/v3/pkg/formats/rtph264"
	"github.com/bluenviron/gortsplib/v3/pkg/formats/rtpsimpleaudio"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

var ErrNeedMoreFrames = errors.New("need more frames")

type UnPacker struct {
	reorderer   *rtpreorderer.Reorderer
	payloadType base.AvPacketPt
	clockRate   uint32
	dec         IRtpDecoder
	pktChan     chan<- base.AvPacket
}

func NewUnPacker(mimeType string, clockRate uint32, pktChan chan<- base.AvPacket) *UnPacker {
	un := &UnPacker{
		reorderer: rtpreorderer.New(),
		clockRate: clockRate,
		pktChan:   pktChan,
	}

	// TODO 支持opus
	switch mimeType {
	case webrtc.MimeTypeH264:
		un.payloadType = base.AvPacketPtAvc
		un.dec = NewH264RtpDecoder()
	case webrtc.MimeTypePCMA:
		un.payloadType = base.AvPacketPtG711A
		un.dec = NewG711RtpDecoder()
	case webrtc.MimeTypePCMU:
		un.payloadType = base.AvPacketPtG711U
		un.dec = NewG711RtpDecoder()
	default:
		nazalog.Errorf("invalid mimeType:%s", mimeType)
		return nil
	}

	return un
}

func (un *UnPacker) UnPack(pkt *rtp.Packet) (err error) {
	packets, lost := un.reorderer.Process(pkt)
	if lost != 0 {
		nazalog.Error("rtp lost")
		return
	}

	for _, rtppkt := range packets {
		frame, pts, err := un.dec.Decode(rtppkt)
		if err != nil {
			if err != ErrNeedMoreFrames {
				nazalog.Error(err)
				return err
			}

			continue
		}

		var pkt base.AvPacket
		pkt.PayloadType = un.payloadType
		pkt.Timestamp = int64(pts / time.Millisecond)
		pkt.Pts = pkt.Timestamp
		pkt.Payload = append(pkt.Payload, frame...)

		un.pktChan <- pkt
	}

	return
}

type IRtpDecoder interface {
	Decode(pkt *rtp.Packet) ([]byte, time.Duration, error)
}

type H264RtpDecoder struct {
	IRtpDecoder
	dec *rtph264.Decoder
}

func NewH264RtpDecoder() *H264RtpDecoder {
	var forma formats.H264
	dec, err := forma.CreateDecoder2()
	if err != nil {
		nazalog.Error(err)
		return nil
	}

	return &H264RtpDecoder{
		dec: dec,
	}
}

func (r *H264RtpDecoder) Decode(pkt *rtp.Packet) ([]byte, time.Duration, error) {
	nalus, pts, err := r.dec.DecodeUntilMarker(pkt)
	if err != nil {
		if err == rtph264.ErrNonStartingPacketAndNoPrevious || err == rtph264.ErrMorePacketsNeeded {
			err = ErrNeedMoreFrames
		}

		return nil, 0, err
	}

	var frame []byte
	for _, nalu := range nalus {
		frame = append(frame, avc.NaluStartCode4...)
		frame = append(frame, nalu...)
	}

	if len(frame) == 0 {
		err = fmt.Errorf("invalid frame")
		return nil, 0, err
	}

	return frame, pts, nil
}

type G711RtpDecoder struct {
	IRtpDecoder
	dec *rtpsimpleaudio.Decoder
}

func NewG711RtpDecoder() *G711RtpDecoder {
	var forma formats.G711
	dec, err := forma.CreateDecoder2()
	if err != nil {
		nazalog.Error(err)
		return nil
	}

	return &G711RtpDecoder{
		dec: dec,
	}
}

func (r *G711RtpDecoder) Decode(pkt *rtp.Packet) ([]byte, time.Duration, error) {
	frame, pts, err := r.dec.Decode(pkt)
	if err != nil {
		nazalog.Error(err)
		return nil, 0, err
	}

	return frame, pts, nil
}
