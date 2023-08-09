package hook

import (
	"sync"

	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

type IHookSessionSubscriber interface {
	OnMsg(msg base.RtmpMsg)
	OnStop()
}

type HookSession struct {
	uniqueKey   string
	streamName  string
	consumers   sync.Map
	videoheader *base.RtmpMsg
	audioheader *base.RtmpMsg
}

type consumerInfo struct {
	subscriber   IHookSessionSubscriber
	hasSendVideo bool
}

func NewHookSession(uniqueKey, streamName string) *HookSession {
	s := &HookSession{
		uniqueKey:  uniqueKey,
		streamName: streamName,
	}

	nazalog.Infof("create hook session, uniqueKey:%s, streamName:%s", uniqueKey, streamName)

	GetHookSessionManagerInstance().SetHookSession(streamName, s)
	return s
}

func (session *HookSession) OnMsg(msg base.RtmpMsg) {

	if msg.IsVideoKeySeqHeader() {
		session.cacheVideoHeaderMsg(&msg)
	}

	if msg.Header.MsgTypeId == base.RtmpTypeIdAudio {
		if msg.IsAacSeqHeader() {
			session.cacheAudioHeaderMsg(&msg)
		}

		if msg.AudioCodecId() == base.RtmpSoundFormatG711A || msg.AudioCodecId() == base.RtmpSoundFormatG711U {
			session.cacheAudioHeaderMsg(&msg)
		}
	}

	// TODO:做缓存处理/纯音频
	session.consumers.Range(func(key, value interface{}) bool {
		c := value.(*consumerInfo)
		if msg.Header.MsgTypeId == base.RtmpTypeIdVideo {
			if !c.hasSendVideo {
				if msg.IsVideoKeyNalu() {
					c.hasSendVideo = true
				} else {
					return true
				}
			}

			c.subscriber.OnMsg(msg)
		} else if msg.Header.MsgTypeId == base.RtmpTypeIdAudio {
			if c.hasSendVideo {
				c.subscriber.OnMsg(msg)
			}
		}
		return true
	})
}

func (session *HookSession) cacheVideoHeaderMsg(msg *base.RtmpMsg) {
	session.videoheader = msg
}

func (session *HookSession) cacheAudioHeaderMsg(msg *base.RtmpMsg) {
	session.audioheader = msg
}

func (session *HookSession) OnStop() {
	nazalog.Debugf("OnStop, uniqueKey:%s, streamName:%s", session.uniqueKey, session.streamName)
	session.consumers.Range(func(key, value interface{}) bool {
		c := value.(*consumerInfo)
		c.subscriber.OnStop()
		return true
	})

	GetHookSessionManagerInstance().RemoveHookSession(session.streamName)
}

func (session *HookSession) AddConsumer(consumerId string, subscriber IHookSessionSubscriber) {

	info := &consumerInfo{
		subscriber: subscriber,
	}

	nazalog.Info("AddConsumer, consumerId:", consumerId)
	session.consumers.Store(consumerId, info)
}

func (session *HookSession) RemoveConsumer(consumerId string) {
	_, ok := session.consumers.Load(consumerId)
	if ok {
		nazalog.Info("RemoveConsumer, consumerId:", consumerId)
		session.consumers.Delete(consumerId)
	}
}

func (session *HookSession) GetVideoSeqHeaderMsg() *base.RtmpMsg {
	return session.videoheader
}

func (session *HookSession) GetAudioSeqHeaderMsg() *base.RtmpMsg {
	return session.audioheader
}
