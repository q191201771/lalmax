package rtc

import (
	"fmt"
	"math/rand"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/naza/pkg/nazalog"
)

type Packer struct {
	enc IRtpEncoder
}

func NewPacker(mimeType string, codec []byte) *Packer {
	p := &Packer{}

	switch mimeType {
	case webrtc.MimeTypeH264:
		p.enc = NewH264RtpEncoder(codec)
	case webrtc.MimeTypePCMA:
		p.enc = NewG711RtpEncoder(8)
	case webrtc.MimeTypePCMU:
		p.enc = NewG711RtpEncoder(0)
	}
	return p
}

func (p *Packer) Encode(data []byte, pts uint32) ([]*rtp.Packet, error) {
	return p.enc.Encode(data, pts)
}

type IRtpEncoder interface {
	Encode(data []byte, pts uint32) ([]*rtp.Packet, error)
}

type H264RtpEncoder struct {
	IRtpEncoder
	codec      []byte
	packetizer rtp.Packetizer
}

func NewH264RtpEncoder(codec []byte) *H264RtpEncoder {
	spspps, err := avc.SpsPpsSeqHeader2Annexb(codec)
	if err != nil {
		nazalog.Error(err)
		return nil
	}

	enc := &H264RtpEncoder{
		codec:      codec,
		packetizer: rtp.NewPacketizer(1400, 96, rand.Uint32(), &codecs.H264Payloader{}, rtp.NewRandomSequencer(), 90000),
	}

	enc.packetizer.Packetize(spspps, 0)
	return enc
}

func (enc *H264RtpEncoder) Encode(data []byte, pts uint32) ([]*rtp.Packet, error) {
	nalus, err := avc.Avcc2Annexb(data)
	if err != nil {
		nazalog.Error(err)
		return nil, err
	}

	pkts := enc.packetizer.Packetize(nalus, pts)
	if len(pkts) == 0 {
		return nil, fmt.Errorf("Packetize failed")
	}

	return pkts, nil
}

type G711RtpEncoder struct {
	IRtpEncoder
	packetizer rtp.Packetizer
}

func NewG711RtpEncoder(pt uint8) *G711RtpEncoder {
	// TODO 暂时采样率设置为8000
	return &G711RtpEncoder{
		packetizer: rtp.NewPacketizer(1400, pt, rand.Uint32(), &codecs.G711Payloader{}, rtp.NewRandomSequencer(), 8000),
	}
}

func (enc *G711RtpEncoder) Encode(data []byte, pts uint32) ([]*rtp.Packet, error) {
	pkts := enc.packetizer.Packetize(data, pts)
	if len(pkts) == 0 {
		return nil, fmt.Errorf("Packetize failed")
	}

	return pkts, nil
}
