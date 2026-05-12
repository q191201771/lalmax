package rtc

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"github.com/gofrs/uuid"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/httpflv"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/lal/pkg/remux"
	maxlogic "github.com/q191201771/lalmax/logic"
	"github.com/q191201771/naza/pkg/nazalog"
	"github.com/smallnest/chanx"
)

type jessibucaSession struct {
	group        *maxlogic.Group
	pc           *peerConnection
	subscriberId string
	lalServer    logic.ILalServer
	videoTrack   *webrtc.TrackLocalStaticRTP
	audioTrack   *webrtc.TrackLocalStaticRTP
	videopacker  *Packer
	audiopacker  *Packer
	msgChan      *chanx.UnboundedChan[base.RtmpMsg]
	closeChan    chan bool
	remoteSafari bool
	DC           *webrtc.DataChannel
	streamId     string
	cancel       context.CancelFunc
	stopOne      sync.Once
	wroteBytes   atomic.Uint64
	remoteAddr   atomic.Value
}

func NewJessibucaSession(appName, streamid string, writeChanSize int, pc *peerConnection, lalServer logic.ILalServer) *jessibucaSession {
	ok, group := maxlogic.GetGroupManagerInstance().GetGroup(maxlogic.NewStreamKey(appName, streamid))
	if !ok {
		nazalog.Errorf("not found stream, appName:%s, streamid:%s", appName, streamid)
		return nil
	}

	u, _ := uuid.NewV4()
	ctx, cancel := context.WithCancel(context.Background())
	return &jessibucaSession{
		group:        group,
		pc:           pc,
		lalServer:    lalServer,
		subscriberId: u.String(),
		streamId:     streamid,
		cancel:       cancel,
		msgChan:      chanx.NewUnboundedChan[base.RtmpMsg](ctx, writeChanSize),
		closeChan:    make(chan bool, 1),
	}
}
func (conn *jessibucaSession) createDataChannel() (err error) {
	if conn.DC != nil {
		return nil
	}
	conn.DC, err = conn.pc.CreateDataChannel(conn.streamId, nil)
	return
}
func (conn *jessibucaSession) GetAnswerSDP(offer string) (sdp string) {
	var err error
	err = conn.createDataChannel()
	if err != nil {
		nazalog.Error(err)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(conn.pc.PeerConnection)

	conn.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	})

	answer, err := conn.pc.CreateAnswer(nil)
	if err != nil {
		nazalog.Error(err)
		return
	}

	err = conn.pc.SetLocalDescription(answer)
	if err != nil {
		nazalog.Error(err)
		return
	}

	<-gatherComplete

	sdp = conn.pc.LocalDescription().SDP
	return
}

func (conn *jessibucaSession) Run() {
	ok, _ := maxlogic.GetGroupManagerInstance().GetGroup(conn.group.Key())
	if ok {
		conn.group.AddSubscriber(maxlogic.SubscriberInfo{
			SubscriberID: conn.subscriberId,
			Protocol:     maxlogic.SubscriberProtocolJessibuca,
		}, conn)

		conn.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			nazalog.Info("peer connection state: ", state.String())

			switch state {
			case webrtc.PeerConnectionStateConnected:
			case webrtc.PeerConnectionStateDisconnected:
				fallthrough
			case webrtc.PeerConnectionStateFailed:
				fallthrough
			case webrtc.PeerConnectionStateClosed:
				conn.closeChan <- true
			}
		})
		if conn.DC != nil {
			conn.DC.OnOpen(func() {
				if err := conn.DC.Send(httpflv.FlvHeader); err != nil {
					nazalog.Warnf(" stream write videoHeader err:%s", err.Error())
					return
				}
				conn.wroteBytes.Add(uint64(len(httpflv.FlvHeader)))
				conn.refreshRemoteAddr()

				defer func() {
					nazalog.Info("RemoveConsumer, connid:", conn.subscriberId)
					conn.group.RemoveSubscriber(conn.subscriberId)
					conn.DC.Close()
					conn.pc.Close()
					conn.DC = nil
					conn.cancel()
				}()
				for {
					select {
					case msg := <-conn.msgChan.Out:
						lazyRtmpMsg2FlvTag := remux.LazyRtmpMsg2FlvTag{}
						lazyRtmpMsg2FlvTag.Init(msg)
						buf := lazyRtmpMsg2FlvTag.GetEnsureWithoutSdf()
						sendBuf := chunkSlice(buf, math.MaxUint16)
						for _, v := range sendBuf {
							if err := conn.DC.Send(v); err != nil {
								nazalog.Warnf(" stream write msg err:%s", err.Error())
								return
							}
							conn.wroteBytes.Add(uint64(len(v)))
						}

					case <-conn.closeChan:
						return
					}
				}

			})
		}
	}

}

func chunkSlice(slice []byte, size int) [][]byte {
	var chunks [][]byte

	for i := 0; i < len(slice); i += size {
		end := i + size

		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}

	return chunks
}

func (conn *jessibucaSession) OnMsg(msg base.RtmpMsg) {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdMetadata:
		return
	case base.RtmpTypeIdAudio:
		if conn.DC != nil {
			conn.msgChan.In <- msg
		}
	case base.RtmpTypeIdVideo:
		if conn.DC != nil {
			conn.msgChan.In <- msg
		}
	}
}

func (conn *jessibucaSession) OnStop() {
	conn.stopOne.Do(func() {
		conn.closeChan <- true
	})
}

func (conn *jessibucaSession) Close() {
	if conn.DC != nil {
		conn.DC.Close()
	}
	if conn.pc != nil {
		conn.pc.Close()
	}
}

func (conn *jessibucaSession) GetSubscriberStat() maxlogic.SubscriberStat {
	conn.refreshRemoteAddr()
	return maxlogic.SubscriberStat{
		RemoteAddr:    conn.loadRemoteAddr(),
		WroteBytesSum: conn.wroteBytes.Load(),
	}
}

func (conn *jessibucaSession) refreshRemoteAddr() {
	if remoteAddr := conn.currentRemoteAddr(); remoteAddr != "" {
		conn.remoteAddr.Store(remoteAddr)
	}
}

func (conn *jessibucaSession) currentRemoteAddr() string {
	if conn.DC != nil && conn.DC.Transport() != nil {
		if dtls := conn.DC.Transport().Transport(); dtls != nil {
			if remoteAddr := remoteAddrFromDTLSTransport(dtls); remoteAddr != "" {
				return remoteAddr
			}
		}
	}
	if sctp := conn.pc.SCTP(); sctp != nil {
		return remoteAddrFromDTLSTransport(sctp.Transport())
	}
	return ""
}

func (conn *jessibucaSession) loadRemoteAddr() string {
	v := conn.remoteAddr.Load()
	addr, _ := v.(string)
	return addr
}
