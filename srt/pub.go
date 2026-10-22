package srt

import (
	"bufio"
	"context"
	"io"
	"sync"
	"sync/atomic"

	srt "github.com/datarhei/gosrt"
	"github.com/q191201771/lal/pkg/aac"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	maxlogic "github.com/q191201771/lalmax/logic"
	"github.com/q191201771/naza/pkg/nazalog"
	codec "github.com/yapingcat/gomedia/go-codec"
	ts "github.com/yapingcat/gomedia/go-mpeg2"
)

type Publisher struct {
	ctx         context.Context
	srv         *SrtServer
	ss          logic.ICustomizePubSessionContext
	streamName  string
	demuxer     *ts.TSDemuxer
	conn        srt.Conn
	subscribers []*Subscriber
	group       *maxlogic.Group
	publisherID string
	remoteAddr  string
	readBytes   atomic.Uint64
	closeOnce   sync.Once
}

func NewPublisher(ctx context.Context, conn srt.Conn, streamName string, srv *SrtServer) *Publisher {
	pub := &Publisher{
		ctx:        ctx,
		srv:        srv,
		streamName: streamName,
		conn:       conn,
		demuxer:    ts.NewTSDemuxer(),
	}
	if conn != nil && conn.RemoteAddr() != nil {
		pub.remoteAddr = conn.RemoteAddr().String()
	}

	nazalog.Infof("create srt publisher, streamName:%s", streamName)
	return pub
}

func (p *Publisher) SetSession(session logic.ICustomizePubSessionContext) {
	p.ss = session
}

func (p *Publisher) Run() {
	defer p.cleanup()

	if p.ss == nil {
		nazalog.Errorf("srt publisher missing lal session, streamName:%s", p.streamName)
		return
	}

	p.publisherID = p.ss.UniqueKey()
	if ok, group := maxlogic.GetGroupManagerInstance().GetGroupByStreamName(p.streamName); ok {
		p.group = group
	}
	maxlogic.RegisterCustomizePub(p.streamName, p.publisherID, "", p.kick)
	if p.group != nil {
		p.group.AddPublisher(maxlogic.PublisherInfo{
			PublisherID: p.publisherID,
			Protocol:    maxlogic.PublisherProtocolSRT,
			RemoteAddr:  p.remoteAddr,
		}, p)
	}

	audioSampleRate := uint32(0)
	var foundAudio bool
	p.demuxer.OnFrame = func(cid ts.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		var pkt base.AvPacket
		if cid == ts.TS_STREAM_AAC {
			var preAudioDts uint64
			for len(frame) >= aac.AdtsHeaderLength {
				headerLen, frameLen, ok := adtsFrameBounds(frame)
				if !ok {
					return
				}
				if !foundAudio {
					asc, convErr := codec.ConvertADTSToASC(frame[:frameLen])
					if convErr != nil {
						return
					}
					rate, rateOK := aacSampleRateFromIndex(int(asc.Sample_freq_index))
					if !rateOK {
						return
					}
					p.ss.FeedAudioSpecificConfig(asc.Encode())
					audioSampleRate = rate
					foundAudio = true
				}
				if preAudioDts == 0 {
					preAudioDts = dts
				} else {
					duration, ok := aacFrameDurationMs(audioSampleRate)
					if !ok {
						return
					}
					preAudioDts += duration
				}

				aacPacket := base.AvPacket{
					Timestamp:   int64(preAudioDts),
					PayloadType: base.AvPacketPtAac,
					Pts:         int64(preAudioDts),
				}
				aacPacket.Payload = frame[headerLen:frameLen]
				p.ss.FeedAvPacket(aacPacket)
				frame = frame[frameLen:]
			}
		} else if cid == ts.TS_STREAM_H264 {
			pkt.Payload = frame
			pkt.PayloadType = base.AvPacketPtAvc
			pkt.Pts = int64(pts)
			pkt.Timestamp = int64(dts)
			p.ss.FeedAvPacket(pkt)
		} else if cid == ts.TS_STREAM_H265 {
			pkt.Payload = frame
			pkt.PayloadType = base.AvPacketPtHevc
			pkt.Pts = int64(pts)
			pkt.Timestamp = int64(dts)
			p.ss.FeedAvPacket(pkt)
		}
	}

	reader := &countingReader{r: p.conn, n: &p.readBytes}
	err := p.demuxer.Input(bufio.NewReader(reader))
	if err != nil {
		nazalog.Infof("stream [%s] disconnected", p.streamName)
	}
}

func (p *Publisher) GetPublisherStat() maxlogic.PublisherStat {
	return maxlogic.PublisherStat{
		RemoteAddr:   p.remoteAddr,
		ReadBytesSum: p.readBytes.Load(),
	}
}

func (p *Publisher) kick() {
	_ = p.conn.Close()
}

func (p *Publisher) cleanup() {
	p.closeOnce.Do(func() {
		if p.group != nil && p.publisherID != "" {
			p.group.RemovePublisher(p.publisherID)
		}
		maxlogic.UnregisterCustomizePub(p.publisherID, "")
		_ = p.conn.Close()
		if p.srv != nil && p.ss != nil {
			p.srv.Remove(p.streamName, p.ss)
		}
	})
}

func aacSampleRateFromIndex(idx int) (uint32, bool) {
	if idx < 0 || idx >= len(codec.AAC_Sampling_Idx) {
		return 0, false
	}
	rate := codec.AACSampleIdxToSample(idx)
	if rate <= 0 {
		return 0, false
	}
	return uint32(rate), true
}

func aacFrameDurationMs(sampleRate uint32) (uint64, bool) {
	if sampleRate == 0 {
		return 0, false
	}
	return uint64(1024 * 1000 / sampleRate), true
}

func adtsFrameBounds(frame []byte) (headerLen int, frameLen int, ok bool) {
	if len(frame) < aac.AdtsHeaderLength || frame[0] != 0xFF || frame[1]&0xF6 != 0xF0 {
		return 0, 0, false
	}
	headerLen = aac.AdtsHeaderLength
	if frame[1]&0x01 == 0 {
		headerLen += 2
	}
	frameLen = int(frame[3]&0x03)<<11 | int(frame[4])<<3 | int(frame[5]>>5)
	if frameLen < headerLen || frameLen > len(frame) {
		return 0, 0, false
	}
	return headerLen, frameLen, true
}

type countingReader struct {
	r io.Reader
	n *atomic.Uint64
}

func (cr *countingReader) Read(b []byte) (int, error) {
	n, err := cr.r.Read(b)
	if n > 0 {
		cr.n.Add(uint64(n))
	}
	return n, err
}
