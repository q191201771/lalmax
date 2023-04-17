package srt

import (
	"context"
	"lalmax/hook"

	"github.com/haivision/srtgo"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
	uuid "github.com/satori/go.uuid"
	codec "github.com/yapingcat/gomedia/go-codec"
	flv "github.com/yapingcat/gomedia/go-flv"
	ts "github.com/yapingcat/gomedia/go-mpeg2"
)

type Subscriber struct {
	ctx             context.Context
	socket          *srtgo.SrtSocket
	streamid        *StreamID
	muxer           *ts.TSMuxer
	hasInit         bool
	videoPid        uint16
	audioPid        uint16
	flvVideoDemuxer flv.VideoTagDemuxer
	flvAudioDemuxer flv.AudioTagDemuxer
	videodts        uint32
	audiodts        uint32
	subscriberId    string
}

func NewSubscriber(ctx context.Context, socket *srtgo.SrtSocket, streamid *StreamID) *Subscriber {

	sub := &Subscriber{
		ctx:          ctx,
		socket:       socket,
		streamid:     streamid,
		muxer:        ts.NewTSMuxer(),
		subscriberId: uuid.NewV4().String(),
	}

	nazalog.Infof("create srt subscriber, streamName:%s, subscriberId:%s", sub.streamid.Host, sub.subscriberId)

	return sub
}

func (s *Subscriber) Run() {
	ok, session := hook.GetHookSessionManagerInstance().GetHookSession(s.streamid.Host)
	if ok {
		var err error

		session.AddConsumer(s.subscriberId, s)
		s.muxer.OnPacket = func(tsPacket []byte) {
			defer func() {
				if err != nil {
					nazalog.Info("close srt socket")
					s.socket.Close()
				}

			}()

			select {
			case <-s.ctx.Done():
				return
			default:
			}

			if _, err = s.socket.Write(tsPacket); err != nil {
				session.RemoveConsumer(s.subscriberId)
				return
			}
		}
	} else {
		nazalog.Warnf("not found hook session, streamName:%s", s.streamid.Host)
		s.socket.Close()
	}
}

func (s *Subscriber) OnMsg(msg base.RtmpMsg) {
	var err error
	if !s.hasInit {
		ok, session := hook.GetHookSessionManagerInstance().GetHookSession(s.streamid.Host)
		if ok {
			videoheader := session.GetVideoSeqHeaderMsg()
			if videoheader != nil {
				if videoheader.IsAvcKeySeqHeader() {
					s.videoPid = s.muxer.AddStream(ts.TS_STREAM_H264)
					s.flvVideoDemuxer = flv.CreateFlvVideoTagHandle(flv.FLV_AVC)
				} else {
					s.videoPid = s.muxer.AddStream(ts.TS_STREAM_H265)
					s.flvVideoDemuxer = flv.CreateFlvVideoTagHandle(flv.FLV_HEVC)
				}

				s.flvVideoDemuxer.OnFrame(func(codecid codec.CodecID, b []byte, cts int) {
					s.muxer.Write(s.videoPid, b, uint64(s.videodts)+uint64(cts), uint64(s.videodts))
				})

				if err = s.flvVideoDemuxer.Decode(videoheader.Payload); err != nil {
					nazalog.Error(err)
					return
				}
			}

			audioheader := session.GetAudioSeqHeaderMsg()
			if audioheader != nil {
				if audioheader.IsAacSeqHeader() {
					s.audioPid = s.muxer.AddStream(ts.TS_STREAM_AAC)
				} else {
					return
				}

				s.flvAudioDemuxer = flv.CreateAudioTagDemuxer(flv.FLV_AAC)
				s.flvAudioDemuxer.OnFrame(func(codecid codec.CodecID, b []byte) {
					s.muxer.Write(s.audioPid, b, uint64(s.audiodts), uint64(s.audiodts))
				})

				if err = s.flvAudioDemuxer.Decode(audioheader.Payload); err != nil {
					nazalog.Error(err)
					return
				}
			}
		}

		s.hasInit = true
	}

	if msg.Header.MsgTypeId == base.RtmpTypeIdVideo {
		s.videodts = msg.Dts()
		if s.flvVideoDemuxer != nil {
			if err = s.flvVideoDemuxer.Decode(msg.Payload); err != nil {
				nazalog.Error(err)
				return
			}
		}
	} else {
		s.audiodts = msg.Dts()
		if s.flvAudioDemuxer != nil {
			if err = s.flvAudioDemuxer.Decode(msg.Payload); err != nil {
				nazalog.Error(err)
				return
			}
		}
	}
}

func (s *Subscriber) OnStop() {
	nazalog.Info("srt subscriber onStop")
	s.socket.Close()
}
