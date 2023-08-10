package httpfmp4

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/aac"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/h2645"
	"github.com/q191201771/lal/pkg/hevc"
	"github.com/q191201771/naza/pkg/nazalog"
	uuid "github.com/satori/go.uuid"
	"github.com/yapingcat/gomedia/go-codec"
	"github.com/yapingcat/gomedia/go-mp4"
	"io"
	"lalmax/hook"
	"net"
	"net/http"
	"strings"
)

type fmp4WriterSeeker struct {
	buffer []byte
	offset int
}

func newFmp4WriterSeeker(capacity int) *fmp4WriterSeeker {
	return &fmp4WriterSeeker{
		buffer: make([]byte, 0, capacity),
		offset: 0,
	}
}

func (fws *fmp4WriterSeeker) Write(p []byte) (n int, err error) {
	if cap(fws.buffer)-fws.offset >= len(p) {
		if len(fws.buffer) < fws.offset+len(p) {
			fws.buffer = fws.buffer[:fws.offset+len(p)]
		}
		copy(fws.buffer[fws.offset:], p)
		fws.offset += len(p)
		return len(p), nil
	}
	tmp := make([]byte, len(fws.buffer), cap(fws.buffer)+len(p)*2)
	copy(tmp, fws.buffer)
	if len(fws.buffer) < fws.offset+len(p) {
		tmp = tmp[:fws.offset+len(p)]
	}
	copy(tmp[fws.offset:], p)
	fws.buffer = tmp
	fws.offset += len(p)
	return len(p), nil
}

func (fws *fmp4WriterSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		if fws.offset+int(offset) > len(fws.buffer) {
			return -1, errors.New(fmt.Sprint("SeekCurrent out of range", len(fws.buffer), offset, fws.offset))
		}
		fws.offset += int(offset)
		return int64(fws.offset), nil
	} else if whence == io.SeekStart {
		if offset > int64(len(fws.buffer)) {
			return -1, errors.New(fmt.Sprint("SeekStart out of range", len(fws.buffer), offset, fws.offset))
		}
		fws.offset = int(offset)
		return offset, nil
	} else {
		return 0, errors.New("unsupport SeekEnd")
	}
}

type Frame struct {
	PayloadType base.AvPacketPt
	Dts         uint32
	Cts         uint32
	Payload     []byte
}

type HttpFmp4Session struct {
	streamid     string
	hooks        *hook.HookSession
	subscriberId string
	spspps       []byte
	asc          []byte
	avPacketChan chan Frame
	audioTrakId  uint32
	videoTrakId  uint32
	w            gin.ResponseWriter
	muxer        *mp4.Movmuxer
	fws          *fmp4WriterSeeker
	initVideoDts uint32
	initAudioDts uint32
}

func NewHttpFmp4Session(streamid string) *HttpFmp4Session {

	streamid = strings.TrimSuffix(streamid, ".mp4")
	session := &HttpFmp4Session{
		streamid:     streamid,
		subscriberId: uuid.NewV4().String(),
		avPacketChan: make(chan Frame, 100),
		fws:          newFmp4WriterSeeker(1024 * 1024),
	}

	session.muxer, _ = mp4.CreateMp4Muxer(session.fws, mp4.WithMp4Flag(mp4.MP4_FLAG_FRAGMENT))

	nazalog.Info("create http fmp4 seesion, streamid:", streamid)

	return session
}

func (session *HttpFmp4Session) handleSession(c *gin.Context) {
	ok, hooksession := hook.GetHookSessionManagerInstance().GetHookSession(session.streamid)
	if !ok {
		nazalog.Error("stream is not found, streamid:", session.streamid)
		c.Status(http.StatusNotFound)
		return
	}

	session.hooks = hooksession
	session.w = c.Writer
	videoHeader := hooksession.GetVideoSeqHeaderMsg()
	if videoHeader != nil {
		codec := videoHeader.VideoCodecId()
		switch codec {
		case base.RtmpCodecIdAvc:
			session.videoTrakId = session.muxer.AddVideoTrack(mp4.MP4_CODEC_H264)
			sps, pps, err := avc.ParseSpsPpsFromSeqHeader(videoHeader.Payload)
			if err != nil {
				nazalog.Error("ParseSpsPpsFromSeqHeader failed, err:", err)
				break
			}

			session.spspps = avc.BuildSpsPps2Annexb(sps, pps)
			spspps := make([]byte, len(session.spspps))
			copy(spspps, session.spspps)
			session.muxer.Write(session.videoTrakId, spspps, uint64(videoHeader.Pts()), uint64(videoHeader.Dts()))

		case base.RtmpCodecIdHevc:
			session.videoTrakId = session.muxer.AddVideoTrack(mp4.MP4_CODEC_H265)

			vps, sps, pps, err := hevc.ParseVpsSpsPpsFromSeqHeaderWithoutMalloc(videoHeader.Payload)
			if err != nil {
				nazalog.Error("ParseVpsSpsPpsFromSeqHeaderWithoutMalloc failed, err:", err)
				break
			}
			spspps := make([]byte, len(session.spspps))
			copy(spspps, session.spspps)
			session.spspps, err = hevc.BuildVpsSpsPps2Annexb(vps, sps, pps)
			if err != nil {
				nazalog.Error("BuildVpsSpsPps2Annexb failed, err:", err)
				break
			}

			session.muxer.Write(session.videoTrakId, session.spspps, uint64(videoHeader.Pts()), uint64(videoHeader.Dts()))
		default:
			nazalog.Error("unsupport video codec:", codec)
		}
	}

	audioHeader := hooksession.GetAudioSeqHeaderMsg()
	if audioHeader != nil {
		codec := audioHeader.AudioCodecId()
		switch codec {
		case base.RtmpSoundFormatAac:
			session.asc = make([]byte, len(audioHeader.Payload[2:]))
			copy(session.asc, audioHeader.Payload[2:])

			ascCtx, err := aac.NewAscContext(audioHeader.Payload[2:])
			if err != nil {
				nazalog.Error(err)
				return
			}

			channelCount := ascCtx.ChannelConfiguration
			sampleRate, _ := ascCtx.GetSamplingFrequency()
			session.audioTrakId = session.muxer.AddAudioTrack(mp4.MP4_CODEC_AAC, mp4.WithExtraData(audioHeader.Payload[2:]), mp4.WithAudioChannelCount(channelCount), mp4.WithAudioSampleRate(uint32(sampleRate)))
		case base.RtmpSoundFormatG711A:
			session.audioTrakId = session.muxer.AddAudioTrack(mp4.MP4_CODEC_G711A, mp4.WithAudioChannelCount(1), mp4.WithAudioSampleRate(8000))
		case base.RtmpSoundFormatG711U:
			session.audioTrakId = session.muxer.AddAudioTrack(mp4.MP4_CODEC_G711U, mp4.WithAudioChannelCount(1), mp4.WithAudioSampleRate(8000))
		default:
			nazalog.Error("unsupport audio codec:", codec)
		}
	}

	if videoHeader == nil && audioHeader == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", "video/mp4")
	c.Header("Connection", "close")
	c.Header("Expires", "-1")
	h, ok := session.w.(http.Hijacker)
	if !ok {
		nazalog.Error("gin response does not implement http.Hijacker")
		return
	}

	conn, bio, err := h.Hijack()
	if err != nil {
		nazalog.Errorf("hijack failed. err=%+v", err)
		return
	}
	if bio.Reader.Buffered() != 0 || bio.Writer.Buffered() != 0 {
		nazalog.Errorf("hijack but buffer not empty. rb=%d, wb=%d", bio.Reader.Buffered(), bio.Writer.Buffered())
	}
	if err = session.writeHttpHeader(conn, session.w.Header()); err != nil {
		nazalog.Errorf("session writeHttpHeader. err=%+v", err)
		return
	}
	session.hooks.AddConsumer(session.subscriberId, session)
	defer session.hooks.RemoveConsumer(session.subscriberId)
	connCloseErr := make(chan error, 1)
	go func() {
		readBuf := make([]byte, 1024)
		if _, err = conn.Read(readBuf); err != nil {
			connCloseErr <- err
		}
	}()
	session.muxer.WriteInitSegment(conn)
	for {
		writeFragment := func(data []byte) error {
			if _, err = conn.Write(data); err != nil {
				return err
			}
			return nil
		}

		select {
		case pkt := <-session.avPacketChan:
			if pkt.PayloadType == base.AvPacketPtAac || pkt.PayloadType == base.AvPacketPtG711A || pkt.PayloadType == base.AvPacketPtG711U {
				if session.initAudioDts == 0 {
					session.initAudioDts = pkt.Dts
				}

				dts := pkt.Dts - session.initAudioDts
				pts := dts

				session.muxer.Write(session.audioTrakId, pkt.Payload, uint64(pts), uint64(dts))
				session.muxer.FlushFragment()
				err = writeFragment(session.fws.buffer)
				if err != nil {
					return
				}

				fws := newFmp4WriterSeeker(1024 * 1024)
				session.muxer.ReBindWriter(fws)
				session.fws = fws
			} else if pkt.PayloadType == base.AvPacketPtAvc || pkt.PayloadType == base.AvPacketPtHevc {
				if session.initVideoDts == 0 {
					session.initVideoDts = pkt.Dts
				}

				dts := pkt.Dts - session.initVideoDts
				pts := dts + pkt.Cts

				session.muxer.Write(session.videoTrakId, pkt.Payload, uint64(pts), uint64(dts))
				session.muxer.FlushFragment()

				err = writeFragment(session.fws.buffer)
				if err != nil {
					return
				}

				fws := newFmp4WriterSeeker(1024 * 1024)
				session.muxer.ReBindWriter(fws)
				session.fws = fws
			}
		case <-connCloseErr:
			session.OnStop()
		}
	}

}
func (session *HttpFmp4Session) writeHttpHeader(conn net.Conn, header http.Header) error {
	p := make([]byte, 0, 1024)
	p = append(p, []byte("HTTP/1.1 200 OK\r\n")...)
	for k, vs := range header {
		for _, v := range vs {
			p = append(p, k...)
			p = append(p, ": "...)
			for i := 0; i < len(v); i++ {
				b := v[i]
				if b <= 31 {
					// prevent response splitting.
					b = ' '
				}
				p = append(p, b)
			}
			p = append(p, "\r\n"...)
		}
	}
	p = append(p, "\r\n"...)
	_, err := conn.Write(p)
	return err
}
func (session *HttpFmp4Session) OnMsg(msg base.RtmpMsg) {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdMetadata:
		return
	case base.RtmpTypeIdAudio:
		session.AudioMsg2AvPacket(msg)
	case base.RtmpTypeIdVideo:
		session.VideoMsg2AvPacket(msg)
	}
}

func (session *HttpFmp4Session) OnStop() {
	session.hooks.RemoveConsumer(session.streamid)
}

func (session *HttpFmp4Session) VideoMsg2AvPacket(msg base.RtmpMsg) {
	if len(msg.Payload) < 5 {
		return
	}

	isH264 := msg.VideoCodecId() == base.RtmpCodecIdAvc

	var out []byte
	var vps, sps, pps []byte
	//appendSpsppsFlag := false
	h2645.IterateNaluAvcc(msg.Payload[5:], func(nal []byte) {
		nalType := h2645.ParseNaluType(isH264, nal[0])

		if isH264 {
			if nalType == h2645.H264NaluTypeSps {
				sps = nal
			} else if nalType == h2645.H264NaluTypePps {
				pps = nal
				if len(sps) != 0 && len(pps) != 0 {
					session.spspps = session.spspps[0:0]
					session.spspps = append(session.spspps, h2645.NaluStartCode4...)
					session.spspps = append(session.spspps, sps...)
					session.spspps = append(session.spspps, h2645.NaluStartCode4...)
					session.spspps = append(session.spspps, pps...)
				}
			} else if nalType == h2645.H264NaluTypeIdrSlice {
				//if !appendSpsppsFlag {
				//	out = append(out, session.spspps...)
				//	appendSpsppsFlag = true
				//}

				out = append(out, h2645.NaluStartCode4...)
				out = append(out, nal...)
			} else if nalType == h2645.H264NaluTypeSei {
				// 丢弃SEI
			} else {
				out = append(out, h2645.NaluStartCode4...)
				out = append(out, nal...)
			}
		} else {
			if nalType == h2645.H265NaluTypeVps {
				vps = nal
			} else if nalType == h2645.H265NaluTypeSps {
				sps = nal
			} else if nalType == h2645.H265NaluTypePps {
				pps = nal
				if len(vps) != 0 && len(sps) != 0 && len(pps) != 0 {
					session.spspps = session.spspps[0:0]
					session.spspps = append(session.spspps, h2645.NaluStartCode4...)
					session.spspps = append(session.spspps, vps...)
					session.spspps = append(session.spspps, h2645.NaluStartCode4...)
					session.spspps = append(session.spspps, sps...)
					session.spspps = append(session.spspps, h2645.NaluStartCode4...)
					session.spspps = append(session.spspps, pps...)
				}
			} else if h2645.H265IsIrapNalu(nalType) {
				//if !appendSpsppsFlag {
				//	out = append(out, session.spspps...)
				//	appendSpsppsFlag = true
				//}

				out = append(out, h2645.NaluStartCode4...)
				out = append(out, nal...)
			} else if nalType == h2645.H265NaluTypeSei {
				// 丢弃SEI
			} else {
				out = append(out, h2645.NaluStartCode4...)
				out = append(out, nal...)
			}
		}
	})

	if len(out) > 0 {
		pkt := Frame{
			Dts:     msg.Dts(),
			Cts:     msg.Cts(),
			Payload: out,
		}
		if isH264 {
			pkt.PayloadType = base.AvPacketPtAvc
		} else {
			pkt.PayloadType = base.AvPacketPtHevc
		}

		session.avPacketChan <- pkt
	}
}

func (session *HttpFmp4Session) AudioMsg2AvPacket(msg base.RtmpMsg) {
	var out []byte
	var audiotype base.AvPacketPt
	codecid := msg.AudioCodecId()
	if codecid == base.RtmpSoundFormatAac {
		audiotype = base.AvPacketPtAac

		if !msg.IsAacSeqHeader() {
			data := msg.Payload[2:]
			adts, err := codec.ConvertASCToADTS(session.asc, len(data)+7)
			if err == nil {
				out = append(adts.Encode(), data...)
			}
		}
	} else if codecid == base.RtmpSoundFormatG711A {
		audiotype = base.AvPacketPtG711A
		out = append(out, msg.Payload[1:]...)
	} else if codecid == base.RtmpSoundFormatG711U {
		audiotype = base.AvPacketPtG711U
		out = append(out, msg.Payload[1:]...)
	} else {
		nazalog.Error("invalid audio codec type:", codecid)
		return
	}

	if len(out) != 0 {
		pkt := Frame{
			PayloadType: audiotype,
			Dts:         msg.Dts(),
			Cts:         msg.Cts(),
			Payload:     out,
		}

		session.avPacketChan <- pkt
	}
}
