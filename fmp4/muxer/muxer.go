package muxer

import (
	"fmt"

	"github.com/q191201771/lal/pkg/aac"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

type Fmp4Muxer struct {
	audioTimeScale uint32
	videoheader    *base.RtmpMsg
	audioheader    *base.RtmpMsg
	initFmp4Data   []byte
	videoTrack     *Track
	videoTrackId   int
	audioTrack     *Track
	audioTrackId   int
}

func NewFmp4Muxer(videoheader *base.RtmpMsg, audioheader *base.RtmpMsg) *Fmp4Muxer {
	m := &Fmp4Muxer{
		videoheader: videoheader,
		audioheader: audioheader,
	}

	trackID := 1
	if m.videoheader != nil {
		m.videoTrack = &Track{
			ID:          trackID,
			TimeScale:   90000,
			CodecHeader: m.videoheader,
		}

		trackID++
	}

	if m.audioheader != nil {
		m.audioTrack = &Track{
			ID:          trackID,
			TimeScale:   m.audioFmp4TimeScale(),
			CodecHeader: m.audioheader,
		}

	}

	return m
}

func (m *Fmp4Muxer) GetInitFmp4() []byte {
	return m.initFmp4Data
}

func (m *Fmp4Muxer) generateInitFmp4() {
	var init Init

	if m.videoTrack != nil {
		init.Tracks = append(init.Tracks, m.videoTrack)
	}

	if m.audioTrack != nil {
		init.Tracks = append(init.Tracks, m.audioTrack)
	}

	var w Buffer
	err := init.Encode(&w)
	if err != nil {
		nazalog.Error("init fmp4 encodec failed, err:", err)
		return
	}

	m.initFmp4Data = w.Bytes()
}

func (m *Fmp4Muxer) audioFmp4TimeScale() uint32 {
	codecId := m.audioheader.AudioCodecId()
	switch codecId {
	case base.RtmpSoundFormatAac:
		// 从asc中获取采样率
		ascCtx, err := aac.NewAscContext(m.audioheader.Payload[2:])
		if err != nil {
			nazalog.Error("new asc context failed, err:", err)
			return 0
		}

		sampleRate, _ := ascCtx.GetSamplingFrequency()
		return uint32(sampleRate)

		// case base.RtmpSoundFormatOpus:
		//	return 48000
	}

	return 0
}

func (m *Fmp4Muxer) WriteVideo(msg base.RtmpMsg) {
	sample, err := NewPartSampleH26x(int32(msg.Cts()), true, msg.Payload[2:])
	if err != nil {
		nazalog.Error("new part sample h26x failed, err:", err)
		return
	}

	fmt.Println("sample duration:", sample.Duration)
}

func (m *Fmp4Muxer) WriteAudio(msg base.RtmpMsg) {

}
