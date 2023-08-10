package srt

import (
	"bufio"
	"context"

	srt "github.com/datarhei/gosrt"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
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
}

func NewPublisher(ctx context.Context, conn srt.Conn, streamName string, srv *SrtServer) *Publisher {
	pub := &Publisher{
		ctx:        ctx,
		srv:        srv,
		streamName: streamName,
		conn:       conn,
		demuxer:    ts.NewTSDemuxer(),
	}

	nazalog.Infof("create srt publisher, streamName:%s", streamName)
	return pub
}

func (p *Publisher) SetSession(session logic.ICustomizePubSessionContext) {
	p.ss = session
}

func (p *Publisher) Run() {
	defer p.conn.Close()

	var foundAudio bool
	p.demuxer.OnFrame = func(cid ts.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		var pkt base.AvPacket
		if cid == ts.TS_STREAM_AAC {
			if !foundAudio {
				asc, _ := codec.ConvertADTSToASC(frame)
				p.ss.FeedAudioSpecificConfig(asc.Encode())
				foundAudio = true
			}

			pkt.Payload = frame[7:]
			pkt.PayloadType = base.AvPacketPtAac
			pkt.Pts = int64(pts)
			pkt.Timestamp = int64(dts)
			p.ss.FeedAvPacket(pkt)
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

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		err := p.demuxer.Input(bufio.NewReader(p.conn))
		if err != nil {
			nazalog.Infof("stream [%s] disconnected", p.streamName)
			p.srv.Remove(p.streamName, p.ss)
			break
		}
	}
}
