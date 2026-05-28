package logic

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/q191201771/lalmax/fmp4/hls"

	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

var _ base.ISession = (*subscriberState)(nil)

const (
	SubscriberProtocolLalmax    = "LALMAX"
	SubscriberProtocolWHEP      = "WHEP"
	SubscriberProtocolJessibuca = "JESSIBUCA"
	SubscriberProtocolHTTPFMP4  = "HTTP-FMP4"
	SubscriberProtocolSRT       = "SRT"

	PublisherProtocolWHIP = "WHIP"
	PublisherProtocolSRT  = "SRT"
)

type Subscriber interface {
	OnMsg(msg base.RtmpMsg)
	OnStop()
}

// 可选接口：订阅者需要区分 GOP 回放和实时帧时实现。
type ReplaySubscriber interface {
	OnReplayStart()
	OnReplayStop()
}

type SubscriberInfo struct {
	SubscriberID string
	Protocol     string
	RemoteAddr   string
}

type PublisherInfo struct {
	PublisherID string
	Protocol    string
	RemoteAddr  string
}

// Group 维护 lalmax 侧订阅者、扩展推流者和回放缓存；lal 原生 pub 仍由 lal 负责。
type Group struct {
	uniqueKey      string
	key            StreamKey
	consumers      sync.Map
	hlssvr         *hls.HlsServer
	manager        *ComplexGroupManager
	hookMux        sync.RWMutex
	activeHookKey  StreamKey
	onActiveHook   func(StreamKey)
	stopHookKey    StreamKey
	onStopHook     func(StreamKey)
	gopCache       *GopCache
	gopCacheMux    sync.RWMutex
	lifecycleMux   sync.RWMutex
	stopOnce       sync.Once
	msgMux         sync.Mutex
	publisherMux   sync.RWMutex
	publisher      *publisherState
	activeHookSent bool
	hasVideo       bool
	closed         atomic.Bool
}

type subscriberState struct {
	key          StreamKey
	subscriber   Subscriber
	statProvider SubscriberStatProvider
	hasSendVideo bool
	replayCache  bool
	writeMux     sync.Mutex
	statMux      sync.Mutex
	stopped      atomic.Bool
	lastStatAt   time.Time

	prevReadBytesSum  uint64
	prevWroteBytesSum uint64

	base.StatSession
}

type publisherState struct {
	key          StreamKey
	statProvider PublisherStatProvider
	statMux      sync.Mutex
	lastStatAt   time.Time

	prevReadBytesSum  uint64
	prevWroteBytesSum uint64

	base.StatSession
}

func (s *subscriberState) AppName() string {
	return s.key.AppName
}

func (s *subscriberState) GetStat() base.StatSession {
	if s == nil {
		return base.StatSession{}
	}
	return s.refreshStat(0)
}

func (s *subscriberState) IsAlive() (readAlive bool, writeAlive bool) {
	return true, true
}

func (s *subscriberState) RawQuery() string {
	return ""
}

func (s *subscriberState) StreamName() string {
	return s.key.StreamName
}

func (s *subscriberState) UniqueKey() string {
	return s.SessionId
}

func (s *subscriberState) UpdateStat(intervalSec uint32) {
	if s == nil {
		return
	}
	s.refreshStat(float64(intervalSec))
}

func (s *subscriberState) Url() string {
	return s.key.String()
}

func newGroup(manager *ComplexGroupManager, uniqueKey string, key StreamKey, hlssvr *hls.HlsServer, gopNum, singleGopMaxFrameNum int) *Group {
	group := &Group{
		uniqueKey: uniqueKey,
		key:       key,
		hlssvr:    hlssvr,
		manager:   manager,
		gopCache:  NewGopCache(gopNum, singleGopMaxFrameNum),
	}

	nazalog.Infof("create group, uniqueKey:%s, streamKey:%s", uniqueKey, key.String())

	return group
}

func (group *Group) initHlsSession() {
	if group != nil && group.hlssvr != nil {
		group.hlssvr.NewHlsSessionWithAppName(group.key.AppName, group.key.StreamName)
	}
}

func (group *Group) waitLifecycleIdle() {
	if group == nil {
		return
	}

	group.lifecycleMux.RLock()
	group.lifecycleMux.RUnlock()
}

func (group *Group) Key() StreamKey {
	return group.key
}

func (group *Group) UniqueKey() string {
	return group.uniqueKey
}

func (group *Group) BindStopHook(key StreamKey, onStop func(StreamKey)) {
	if group == nil {
		return
	}

	group.hookMux.Lock()
	group.stopHookKey = key
	group.onStopHook = onStop
	group.hookMux.Unlock()
}

func (group *Group) BindActiveHook(key StreamKey, onActive func(StreamKey)) {
	if group == nil {
		return
	}

	group.hookMux.Lock()
	group.activeHookKey = key
	group.onActiveHook = onActive
	group.hookMux.Unlock()
}

func (group *Group) OnMsg(msg base.RtmpMsg) {
	group.lifecycleMux.RLock()
	if group.closed.Load() {
		group.lifecycleMux.RUnlock()
		return
	}
	defer group.lifecycleMux.RUnlock()

	if group.hlssvr != nil {
		group.hlssvr.OnMsgWithAppName(group.key.AppName, group.key.StreamName, msg)
	}

	group.msgMux.Lock()
	hasVideo := group.hasVideo
	shouldNotifyActive := false
	consumers := make([]*subscriberState, 0)
	group.consumers.Range(func(key, value interface{}) bool {
		if c, ok := value.(*subscriberState); ok {
			consumers = append(consumers, c)
		}
		return true
	})

	if !group.hasVideo && msg.IsVideoKeyNalu() {
		group.hasVideo = true
	}
	if !group.activeHookSent && isActiveMediaMsg(msg) {
		group.activeHookSent = true
		shouldNotifyActive = true
	}

	group.gopCacheMux.Lock()
	group.gopCache.Feed(msg)
	group.gopCacheMux.Unlock()
	group.msgMux.Unlock()

	if shouldNotifyActive {
		group.hookMux.RLock()
		activeHookKey := group.activeHookKey
		onActiveHook := group.onActiveHook
		group.hookMux.RUnlock()
		if onActiveHook != nil {
			onActiveHook(activeHookKey)
		}
	}

	for _, c := range consumers {
		group.handleSubscriberMsg(c, msg, hasVideo)
	}
}

func isActiveMediaMsg(msg base.RtmpMsg) bool {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdAudio:
		return !msg.IsAacSeqHeader()
	case base.RtmpTypeIdVideo:
		return !msg.IsVideoKeySeqHeader()
	default:
		return false
	}
}

func (group *Group) OnStop() {
	group.stopOnce.Do(func() {
		group.lifecycleMux.Lock()
		group.closed.Store(true)

		if group.hlssvr != nil {
			group.hlssvr.OnStopWithAppName(group.key.AppName, group.key.StreamName)
		}

		consumers := make([]*subscriberState, 0)
		group.consumers.Range(func(key, value interface{}) bool {
			c, ok := value.(*subscriberState)
			if ok {
				consumers = append(consumers, c)
			}
			group.consumers.Delete(key)
			return true
		})

		group.publisherMux.Lock()
		group.publisher = nil
		group.publisherMux.Unlock()

		group.lifecycleMux.Unlock()

		nazalog.Debugf("OnStop, uniqueKey:%s, streamKey:%s", group.uniqueKey, group.key.String())
		for _, c := range consumers {
			c.stopWithNotify()
		}

		if group.manager != nil {
			group.manager.RemoveGroupIfMatch(group.key, group)
		}

		group.hookMux.RLock()
		stopHookKey := group.stopHookKey
		onStopHook := group.onStopHook
		group.hookMux.RUnlock()
		if onStopHook != nil {
			onStopHook(stopHookKey)
		}
	})
}

func (group *Group) AddSubscriber(info SubscriberInfo, subscriber Subscriber) {
	group.AddSubscriberWithReplay(info, subscriber, true)
}

func (group *Group) AddSubscriberWithReplay(info SubscriberInfo, subscriber Subscriber, replayCache bool) {
	if info.SubscriberID == "" {
		nazalog.Warn("AddSubscriber skipped, subscriber id is empty")
		return
	}
	if info.Protocol == "" {
		info.Protocol = SubscriberProtocolLalmax
	}

	group.lifecycleMux.RLock()
	if group.closed.Load() {
		group.lifecycleMux.RUnlock()
		nazalog.Warnf("AddSubscriber skipped, group is closed, streamKey:%s, subscriberId:%s", group.key.String(), info.SubscriberID)
		return
	}
	defer group.lifecycleMux.RUnlock()

	state := &subscriberState{
		key:          group.key,
		subscriber:   subscriber,
		replayCache:  replayCache,
		lastStatAt:   time.Now(),
		statProvider: nil,
		StatSession: base.StatSession{
			SessionId:  info.SubscriberID,
			Protocol:   info.Protocol,
			BaseType:   base.SessionBaseTypeSubStr,
			RemoteAddr: info.RemoteAddr,
			StartTime:  time.Now().Format(time.DateTime),
		},
	}
	if provider, ok := subscriber.(SubscriberStatProvider); ok {
		state.statProvider = provider
	}

	nazalog.Infof("AddSubscriber, streamKey:%s, subscriberId:%s, protocol:%s", group.key.String(), info.SubscriberID, info.Protocol)
	if replayCache {
		// 保证该订阅者先收到缓存 GOP，再收到实时帧。
		state.writeMux.Lock()
	}
	var replayMsgs []base.RtmpMsg

	group.msgMux.Lock()
	if _, loaded := group.consumers.Load(info.SubscriberID); loaded {
		group.msgMux.Unlock()
		if replayCache {
			state.writeMux.Unlock()
		}
		nazalog.Warnf("AddSubscriber skipped, subscriber already exists, streamKey:%s, subscriberId:%s", group.key.String(), info.SubscriberID)
		return
	}
	group.consumers.Store(info.SubscriberID, state)
	if replayCache {
		replayMsgs = group.getGopReplayMessages()
	}
	group.msgMux.Unlock()

	if replayCache {
		group.replayGopMessagesLocked(state, replayMsgs)
		state.writeMux.Unlock()
	}
}

func (group *Group) AddConsumer(consumerID string, subscriber Subscriber) {
	group.AddSubscriber(SubscriberInfo{SubscriberID: consumerID}, subscriber)
}

func (group *Group) AddConsumerWithReplay(consumerID string, subscriber Subscriber, replayCache bool) {
	group.AddSubscriberWithReplay(SubscriberInfo{SubscriberID: consumerID}, subscriber, replayCache)
}

func (group *Group) StatSubscribers() []base.StatSub {
	out := make([]base.StatSub, 0, 10)
	group.consumers.Range(func(key, value any) bool {
		v, ok := value.(*subscriberState)
		if ok {
			out = append(out, base.Session2StatSub(v))
		}
		return true
	})
	return out
}

func (group *Group) GetAllConsumer() []base.StatSub {
	return group.StatSubscribers()
}

func (group *Group) RemoveSubscriber(subscriberID string) {
	value, ok := group.consumers.LoadAndDelete(subscriberID)
	if ok {
		nazalog.Infof("RemoveSubscriber, streamKey:%s, subscriberId:%s", group.key.String(), subscriberID)
		if c, ok := value.(*subscriberState); ok {
			c.stopWithoutNotify()
		}
	}
}

func (group *Group) RemoveConsumer(consumerID string) {
	group.RemoveSubscriber(consumerID)
}

func (group *Group) AddPublisher(info PublisherInfo, provider PublisherStatProvider) {
	if info.PublisherID == "" {
		nazalog.Warn("AddPublisher skipped, publisher id is empty")
		return
	}
	if info.Protocol == "" {
		info.Protocol = PublisherProtocolWHIP
	}

	group.lifecycleMux.RLock()
	if group.closed.Load() {
		group.lifecycleMux.RUnlock()
		nazalog.Warnf("AddPublisher skipped, group is closed, streamKey:%s, publisherId:%s", group.key.String(), info.PublisherID)
		return
	}
	defer group.lifecycleMux.RUnlock()

	state := &publisherState{
		key:          group.key,
		statProvider: provider,
		lastStatAt:   time.Now(),
		StatSession: base.StatSession{
			SessionId:  info.PublisherID,
			Protocol:   info.Protocol,
			BaseType:   base.SessionBaseTypePubStr,
			RemoteAddr: info.RemoteAddr,
			StartTime:  time.Now().Format(time.DateTime),
		},
	}

	group.publisherMux.Lock()
	group.publisher = state
	group.publisherMux.Unlock()

	nazalog.Infof("AddPublisher, streamKey:%s, publisherId:%s, protocol:%s", group.key.String(), info.PublisherID, info.Protocol)
}

func (group *Group) RemovePublisher(publisherID string) {
	group.publisherMux.Lock()
	defer group.publisherMux.Unlock()

	if group.publisher == nil || group.publisher.SessionId != publisherID {
		return
	}

	nazalog.Infof("RemovePublisher, streamKey:%s, publisherId:%s", group.key.String(), publisherID)
	group.publisher = nil
}

func (group *Group) StatPublisher() base.StatPub {
	group.publisherMux.RLock()
	defer group.publisherMux.RUnlock()

	if group.publisher == nil {
		return base.StatPub{}
	}
	return base.StatPub{StatSession: group.publisher.refreshStat(0)}
}

func (group *Group) GetVideoSeqHeaderMsg() *base.RtmpMsg {
	group.gopCacheMux.RLock()
	defer group.gopCacheMux.RUnlock()
	if group.gopCache.videoheader == nil {
		return nil
	}
	m := group.gopCache.videoheader.Clone()
	return &m
}

func (group *Group) GetAudioSeqHeaderMsg() *base.RtmpMsg {
	group.gopCacheMux.RLock()
	defer group.gopCacheMux.RUnlock()
	if group.gopCache.audioheader == nil {
		return nil
	}
	m := group.gopCache.audioheader.Clone()
	return &m
}

func (group *Group) handleSubscriberMsg(c *subscriberState, msg base.RtmpMsg, hasVideo bool) {
	if c == nil {
		return
	}

	c.writeMux.Lock()
	defer c.writeMux.Unlock()

	if c.stopped.Load() || c.subscriber == nil {
		return
	}

	if msg.Header.MsgTypeId == base.RtmpTypeIdVideo {
		if !c.hasSendVideo {
			if !msg.IsVideoKeyNalu() {
				return
			}
			if v := group.GetVideoSeqHeaderMsg(); v != nil {
				if !c.deliverMsg(*v) {
					return
				}
			}
			if v := group.GetAudioSeqHeaderMsg(); v != nil && v.IsAacSeqHeader() {
				if !c.deliverMsg(*v) {
					return
				}
			}
			c.hasSendVideo = true
		}

		c.deliverMsg(msg)
	} else if msg.Header.MsgTypeId == base.RtmpTypeIdAudio {
		if !hasVideo || c.hasSendVideo {
			c.deliverMsg(msg)
		}
	}
}

func (group *Group) replayGopMessagesLocked(c *subscriberState, msgs []base.RtmpMsg) {
	if c == nil || c.subscriber == nil || c.stopped.Load() || c.hasSendVideo || !c.replayCache {
		return
	}

	if len(msgs) == 0 {
		return
	}

	if replaySubscriber, ok := c.subscriber.(ReplaySubscriber); ok {
		replaySubscriber.OnReplayStart()
		defer replaySubscriber.OnReplayStop()
	}

	for _, msg := range msgs {
		if !c.deliverMsg(msg) {
			return
		}
	}
	c.hasSendVideo = true
}

func (s *subscriberState) deliverMsg(msg base.RtmpMsg) bool {
	if s == nil || s.stopped.Load() || s.subscriber == nil {
		return false
	}

	s.subscriber.OnMsg(msg)
	return !s.stopped.Load() && s.subscriber != nil
}

func (s *subscriberState) refreshStat(intervalSec float64) base.StatSession {
	s.statMux.Lock()
	defer s.statMux.Unlock()

	s.refreshStatSnapshotLocked()

	if intervalSec <= 0 {
		if s.lastStatAt.IsZero() {
			s.lastStatAt = time.Now()
			return s.StatSession
		}
		intervalSec = time.Since(s.lastStatAt).Seconds()
		if intervalSec < 1 {
			return s.StatSession
		}
	}

	s.updateBitrateLocked(intervalSec)
	s.lastStatAt = time.Now()
	return s.StatSession
}

func (s *subscriberState) refreshStatSnapshotLocked() {
	if s.statProvider == nil {
		return
	}

	stat := s.statProvider.GetSubscriberStat()
	if stat.RemoteAddr != "" {
		s.StatSession.RemoteAddr = stat.RemoteAddr
	}
	s.StatSession.ReadBytesSum = stat.ReadBytesSum
	s.StatSession.WroteBytesSum = stat.WroteBytesSum
}

func (s *subscriberState) updateBitrateLocked(intervalSec float64) {
	if intervalSec <= 0 {
		return
	}

	readDiff := diffUint64(s.StatSession.ReadBytesSum, s.prevReadBytesSum)
	writeDiff := diffUint64(s.StatSession.WroteBytesSum, s.prevWroteBytesSum)

	s.StatSession.ReadBitrateKbits = bitrateFromBytes(readDiff, intervalSec)
	s.StatSession.WriteBitrateKbits = bitrateFromBytes(writeDiff, intervalSec)
	s.StatSession.BitrateKbits = s.StatSession.WriteBitrateKbits

	s.prevReadBytesSum = s.StatSession.ReadBytesSum
	s.prevWroteBytesSum = s.StatSession.WroteBytesSum
}

func (p *publisherState) refreshStat(intervalSec float64) base.StatSession {
	if p == nil {
		return base.StatSession{}
	}

	p.statMux.Lock()
	defer p.statMux.Unlock()

	p.refreshStatSnapshotLocked()

	if intervalSec <= 0 {
		if p.lastStatAt.IsZero() {
			p.lastStatAt = time.Now()
			return p.StatSession
		}
		intervalSec = time.Since(p.lastStatAt).Seconds()
		if intervalSec < 1 {
			return p.StatSession
		}
	}

	p.updateBitrateLocked(intervalSec)
	p.lastStatAt = time.Now()
	return p.StatSession
}

func (p *publisherState) refreshStatSnapshotLocked() {
	if p.statProvider == nil {
		return
	}

	stat := p.statProvider.GetPublisherStat()
	if stat.RemoteAddr != "" {
		p.StatSession.RemoteAddr = stat.RemoteAddr
	}
	p.StatSession.ReadBytesSum = stat.ReadBytesSum
	p.StatSession.WroteBytesSum = stat.WroteBytesSum
}

func (p *publisherState) updateBitrateLocked(intervalSec float64) {
	if intervalSec <= 0 {
		return
	}

	readDiff := diffUint64(p.StatSession.ReadBytesSum, p.prevReadBytesSum)
	writeDiff := diffUint64(p.StatSession.WroteBytesSum, p.prevWroteBytesSum)

	p.StatSession.ReadBitrateKbits = bitrateFromBytes(readDiff, intervalSec)
	p.StatSession.WriteBitrateKbits = bitrateFromBytes(writeDiff, intervalSec)
	p.StatSession.BitrateKbits = p.StatSession.ReadBitrateKbits

	p.prevReadBytesSum = p.StatSession.ReadBytesSum
	p.prevWroteBytesSum = p.StatSession.WroteBytesSum
}

func bitrateFromBytes(bytes uint64, intervalSec float64) int {
	return int(float64(bytes) * 8 / 1024 / intervalSec)
}

func diffUint64(curr, prev uint64) uint64 {
	if curr < prev {
		return curr
	}
	return curr - prev
}

func (s *subscriberState) stopWithNotify() {
	if s == nil {
		return
	}

	s.writeMux.Lock()
	defer s.writeMux.Unlock()

	if s.stopped.Swap(true) {
		return
	}
	if s.subscriber != nil {
		s.subscriber.OnStop()
		s.subscriber = nil
	}
}

func (s *subscriberState) stopWithoutNotify() {
	if s == nil {
		return
	}

	// 不能在这里获取 writeMux：部分订阅者会在 OnMsg 调用栈内主动移除自己。
	// 只标记停止，避免后续投递；订阅者对象随 state 一起释放。
	s.stopped.Store(true)
}

func (group *Group) getGopReplayMessages() []base.RtmpMsg {
	group.gopCacheMux.RLock()
	defer group.gopCacheMux.RUnlock()

	gopCount := group.gopCache.GetGopCount()
	if gopCount == 0 {
		return nil
	}

	msgs := make([]base.RtmpMsg, 0, gopCount)
	if v := group.gopCache.videoheader; v != nil {
		msgs = append(msgs, v.Clone())
	}
	if v := group.gopCache.audioheader; v != nil && v.IsAacSeqHeader() {
		msgs = append(msgs, v.Clone())
	}
	for i := 0; i < gopCount; i++ {
		for _, item := range group.gopCache.GetGopDataAt(i) {
			msgs = append(msgs, item.Clone())
		}
	}

	return msgs
}
