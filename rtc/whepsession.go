package rtc

import (
	"lalmax/hook"

	"github.com/gofrs/uuid"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type whepSession struct {
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
}

func NewWhepSession(streamid string, pc *peerConnection, lalServer logic.ILalServer) *whepSession {
	ok, session := hook.GetHookSessionManagerInstance().GetHookSession(streamid)
	if !ok {
		nazalog.Error("not found streamid:", streamid)
		return nil
	}

	u, _ := uuid.NewV4()
	return &whepSession{
		hooks:        session,
		pc:           pc,
		lalServer:    lalServer,
		subscriberId: u.String(),
		msgChan:      make(chan base.RtmpMsg, 100),
		closeChan:    make(chan bool),
	}
}

func (conn *whepSession) GetAnswerSDP(offer string) (sdp string) {
	var err error

	videoHeader := conn.hooks.GetVideoSeqHeaderMsg()
	if videoHeader != nil {
		if videoHeader.IsAvcKeySeqHeader() {
			conn.videoTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			_, err = conn.pc.AddTrack(conn.videoTrack)
			if err != nil {
				nazalog.Error(err)
				return
			}

			conn.videopacker = NewPacker(webrtc.MimeTypeH264, videoHeader.Payload)
		}
	}

	audioHeader := conn.hooks.GetAudioSeqHeaderMsg()
	if audioHeader != nil {
		var mimeType string
		audioId := audioHeader.AudioCodecId()
		switch audioId {
		case base.RtmpSoundFormatG711A:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = webrtc.MimeTypePCMA
		case base.RtmpSoundFormatG711U:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = webrtc.MimeTypePCMU
		case base.RtmpSoundFormatAac:
			conn.audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "lalmax")
			if err != nil {
				nazalog.Error(err)
				return
			}

			mimeType = webrtc.MimeTypeOpus
		default:
			nazalog.Error("unsupport audio codeid:", audioId)
		}

		if conn.audioTrack != nil {
			_, err = conn.pc.AddTrack(conn.audioTrack)
			if err != nil {
				nazalog.Error(err)
				return
			}

			conn.audiopacker = NewPacker(mimeType, audioHeader.Payload[2:])
		}
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

func (conn *whepSession) Run() {
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

	for {
		select {
		case msg := <-conn.msgChan:
			if msg.Header.MsgTypeId == base.RtmpTypeIdAudio && conn.audioTrack != nil {
				conn.sendAudio(msg)
			} else if msg.Header.MsgTypeId == base.RtmpTypeIdVideo && conn.videoTrack != nil {
				conn.sendVideo(msg)
			}
		case <-conn.closeChan:
			nazalog.Info("RemoveConsumer, connid:", conn.subscriberId)
			conn.hooks.RemoveConsumer(conn.subscriberId)
			return
		}
	}
}

func (conn *whepSession) OnMsg(msg base.RtmpMsg) {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdMetadata:
		return
	case base.RtmpTypeIdAudio:
		if conn.audioTrack != nil {
			conn.msgChan <- msg
		}
	case base.RtmpTypeIdVideo:
		if msg.IsVideoKeySeqHeader() {
			return
		}
		if conn.videoTrack != nil {
			conn.msgChan <- msg
		}
	}
}

func (conn *whepSession) OnStop() {
	conn.closeChan <- true
}

func (conn *whepSession) sendAudio(msg base.RtmpMsg) {
	if conn.audiopacker != nil {
		pkts, err := conn.audiopacker.Encode(msg)
		if err != nil {
			nazalog.Error(err)
			return
		}

		for _, pkt := range pkts {
			if err := conn.audioTrack.WriteRTP(pkt); err != nil {
				nazalog.Error(err)
				continue
			}
		}
	}
}

func (conn *whepSession) sendVideo(msg base.RtmpMsg) {
	if conn.videopacker != nil {

		pkts, err := conn.videopacker.Encode(msg)
		if err != nil {
			nazalog.Error(err)
			return
		}

		for _, pkt := range pkts {
			if err := conn.videoTrack.WriteRTP(pkt); err != nil {
				continue
			}
		}
	}
}
