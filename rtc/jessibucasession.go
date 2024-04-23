package rtc

import (
	"github.com/gofrs/uuid"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/httpflv"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/lal/pkg/remux"
	"github.com/q191201771/naza/pkg/nazalog"
	"lalmax/hook"
	"math"
)

type jessibucaSession struct {
	hooks        *hook.HookSession
	pc           *peerConnection
	subscriberId string
	lalServer    logic.ILalServer
	videoTrack   *webrtc.TrackLocalStaticRTP
	audioTrack   *webrtc.TrackLocalStaticRTP
	videopacker  *Packer
	audiopacker  *Packer
	msgChan      chan base.RtmpMsg
	closeChan    chan bool
	remoteSafari bool
	DC           *webrtc.DataChannel
	streamId     string
}

func NewJessibucaSession(streamid string, pc *peerConnection, lalServer logic.ILalServer) *jessibucaSession {
	ok, session := hook.GetHookSessionManagerInstance().GetHookSession(streamid)
	if !ok {
		nazalog.Error("not found streamid:", streamid)
		return nil
	}

	u, _ := uuid.NewV4()
	return &jessibucaSession{
		hooks:        session,
		pc:           pc,
		lalServer:    lalServer,
		subscriberId: u.String(),
		streamId:     streamid,
		msgChan:      make(chan base.RtmpMsg, 128),
		closeChan:    make(chan bool, 2),
	}
}

func (conn *jessibucaSession) SetRemoteSafari(val bool) {
	conn.remoteSafari = val
}
func (conn *jessibucaSession) createDataChannel() {
	if conn.DC != nil {
		return
	}
	conn.DC, _ = conn.pc.CreateDataChannel(conn.streamId, nil)
}
func (conn *jessibucaSession) GetAnswerSDP(offer string) (sdp string) {
	var err error
	conn.createDataChannel()

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
	ok, session := hook.GetHookSessionManagerInstance().GetHookSession(conn.streamId)
	if ok {
		conn.hooks.AddConsumer(conn.subscriberId, conn)

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

				videoHeader := session.GetVideoSeqHeaderMsg()
				audioHeader := session.GetAudioSeqHeaderMsg()

				if videoHeader != nil {
					lazyRtmpMsg2FlvTag := remux.LazyRtmpMsg2FlvTag{}
					videoHeader.Header.TimestampAbs = 0
					lazyRtmpMsg2FlvTag.Init(*videoHeader)
					if err := conn.DC.Send(lazyRtmpMsg2FlvTag.GetEnsureWithoutSdf()); err != nil {
						nazalog.Warnf(" stream write videoHeader err:%s", err.Error())
						return
					}
				}
				if audioHeader != nil {
					lazyRtmpMsg2FlvTag := remux.LazyRtmpMsg2FlvTag{}
					audioHeader.Header.TimestampAbs = 0
					lazyRtmpMsg2FlvTag.Init(*audioHeader)
					if err := conn.DC.Send(lazyRtmpMsg2FlvTag.GetEnsureWithoutSdf()); err != nil {
						nazalog.Warnf("stream write audioHeader err:%s", err.Error())
						return
					}
				}
				defer func() {
					conn.DC.Close()
					conn.pc.Close()
					nazalog.Info("RemoveConsumer, connid:", conn.subscriberId)
					conn.hooks.RemoveConsumer(conn.subscriberId)
				}()
				for {
					select {
					case msg := <-conn.msgChan:
						lazyRtmpMsg2FlvTag := remux.LazyRtmpMsg2FlvTag{}
						lazyRtmpMsg2FlvTag.Init(msg)
						buf := lazyRtmpMsg2FlvTag.GetEnsureWithoutSdf()
						sendBuf := chunkSlice(buf, math.MaxUint16)
						for _, v := range sendBuf {
							if err := conn.DC.Send(v); err != nil {
								nazalog.Warnf(" stream write msg err:%s", err.Error())
								return
							}
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
			conn.msgChan <- msg
		}
	case base.RtmpTypeIdVideo:
		if msg.IsVideoKeySeqHeader() {
			return
		}
		if conn.DC != nil {
			conn.msgChan <- msg
		}
	}
}

func (conn *jessibucaSession) OnStop() {
	conn.closeChan <- true
}
