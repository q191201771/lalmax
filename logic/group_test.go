package logic

import (
	"sync"
	"testing"
	"time"

	"github.com/q191201771/lal/pkg/base"
)

type recordSubscriber struct {
	mu        sync.Mutex
	msgs      []base.RtmpMsg
	stopCount int
}

func (s *recordSubscriber) OnMsg(msg base.RtmpMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg.Clone())
}

func (s *recordSubscriber) OnStop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCount++
}

func (s *recordSubscriber) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func (s *recordSubscriber) markerAt(idx int) byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return payloadMarker(s.msgs[idx])
}

func (s *recordSubscriber) stopCountValue() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopCount
}

type blockingSubscriber struct {
	mu        sync.Mutex
	msgs      []base.RtmpMsg
	blocked   chan struct{}
	release   chan struct{}
	replaying bool
	blockOnce sync.Once
}

func newBlockingSubscriber() *blockingSubscriber {
	return &blockingSubscriber{
		blocked: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingSubscriber) OnMsg(msg base.RtmpMsg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg.Clone())
	shouldBlock := s.replaying
	s.mu.Unlock()

	if shouldBlock {
		s.blockOnce.Do(func() {
			close(s.blocked)
			<-s.release
		})
	}
}

func (s *blockingSubscriber) OnStop() {}

func (s *blockingSubscriber) OnReplayStart() {
	s.mu.Lock()
	s.replaying = true
	s.mu.Unlock()
}

func (s *blockingSubscriber) OnReplayStop() {
	s.mu.Lock()
	s.replaying = false
	s.mu.Unlock()
}

func (s *blockingSubscriber) markers() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]byte, 0, len(s.msgs))
	for _, msg := range s.msgs {
		out = append(out, payloadMarker(msg))
	}
	return out
}

type selfRemovingSubscriber struct {
	group *Group
	id    string

	mu   sync.Mutex
	msgs []base.RtmpMsg
}

func (s *selfRemovingSubscriber) OnMsg(msg base.RtmpMsg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg.Clone())
	shouldRemove := len(s.msgs) == 1
	s.mu.Unlock()

	if shouldRemove {
		s.group.RemoveConsumer(s.id)
	}
}

func (s *selfRemovingSubscriber) OnStop() {}

func (s *selfRemovingSubscriber) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs)
}

func (s *selfRemovingSubscriber) markerAt(idx int) byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return payloadMarker(s.msgs[idx])
}

type statSubscriber struct {
	mu   sync.Mutex
	stat SubscriberStat
}

func (s *statSubscriber) OnMsg(msg base.RtmpMsg) {}

func (s *statSubscriber) OnStop() {}

func (s *statSubscriber) GetSubscriberStat() SubscriberStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stat
}

func (s *statSubscriber) setStat(stat SubscriberStat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stat = stat
}

func videoSeqHeader(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
		Payload: []byte{
			base.RtmpAvcKeyFrame,
			base.RtmpAvcPacketTypeSeqHeader,
			0, 0, 0,
			marker,
		},
	}
}

func videoKeyNalu(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
		Payload: []byte{
			base.RtmpAvcKeyFrame,
			base.RtmpAvcPacketTypeNalu,
			0, 0, 0,
			marker,
		},
	}
}

func videoInterNalu(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdVideo},
		Payload: []byte{
			base.RtmpAvcInterFrame,
			base.RtmpAvcPacketTypeNalu,
			0, 0, 0,
			marker,
		},
	}
}

func aacSeqHeader(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdAudio},
		Payload: []byte{
			base.RtmpSoundFormatAac << 4,
			base.RtmpAacPacketTypeSeqHeader,
			marker,
		},
	}
}

func aacRaw(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header: base.RtmpHeader{MsgTypeId: base.RtmpTypeIdAudio},
		Payload: []byte{
			base.RtmpSoundFormatAac << 4,
			base.RtmpAacPacketTypeRaw,
			marker,
		},
	}
}

func g711aAudio(marker byte) base.RtmpMsg {
	return base.RtmpMsg{
		Header:  base.RtmpHeader{MsgTypeId: base.RtmpTypeIdAudio},
		Payload: []byte{base.RtmpSoundFormatG711A<<4 | marker},
	}
}

func payloadMarker(msg base.RtmpMsg) byte {
	return msg.Payload[len(msg.Payload)-1]
}

func newTestGroup(streamName string) *Group {
	group, _ := GetGroupManagerInstance().GetOrCreateGroupByStreamName(streamName, streamName, nil, 1, 0)
	return group
}

func testSubscriberState(t *testing.T, group *Group, subscriberID string) *subscriberState {
	t.Helper()

	value, ok := group.consumers.Load(subscriberID)
	if !ok {
		t.Fatalf("subscriber %s not found", subscriberID)
	}

	state, ok := value.(*subscriberState)
	if !ok {
		t.Fatalf("subscriber %s has unexpected type %T", subscriberID, value)
	}

	return state
}

func TestAddConsumerReplaysCachedGopImmediately(t *testing.T) {
	group := newTestGroup("test-replay")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-replay")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(aacSeqHeader(2))
	group.OnMsg(videoKeyNalu(3))
	group.OnMsg(aacRaw(4))
	group.OnMsg(videoInterNalu(5))

	sub := &recordSubscriber{}
	group.AddConsumer("consumer", sub)

	if sub.len() != 5 {
		t.Fatalf("expected 5 replay messages, got %d", sub.len())
	}

	wantMarkers := []byte{1, 2, 3, 4, 5}
	for i, want := range wantMarkers {
		if got := sub.markerAt(i); got != want {
			t.Fatalf("message %d marker = %d, want %d", i, got, want)
		}
	}
}

func TestVideoSeqHeaderChangeClearsStaleGop(t *testing.T) {
	group := newTestGroup("test-clear")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-clear")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(videoKeyNalu(2))
	group.OnMsg(videoInterNalu(3))
	group.OnMsg(videoSeqHeader(4))

	sub := &recordSubscriber{}
	group.AddConsumer("consumer", sub)
	if sub.len() != 0 {
		t.Fatalf("expected no stale GOP replay after sequence header change, got %d messages", sub.len())
	}

	group.OnMsg(videoKeyNalu(5))
	if sub.len() != 2 {
		t.Fatalf("expected new header and current key frame, got %d messages", sub.len())
	}
	if got := sub.markerAt(0); got != 4 {
		t.Fatalf("header marker = %d, want 4", got)
	}
	if got := sub.markerAt(1); got != 5 {
		t.Fatalf("key frame marker = %d, want 5", got)
	}
}

func TestNonAacAudioIsNotReplayedAsHeader(t *testing.T) {
	group := newTestGroup("test-g711")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-g711")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(videoKeyNalu(2))
	group.OnMsg(g711aAudio(3))

	sub := &recordSubscriber{}
	group.AddConsumer("consumer", sub)

	if sub.len() != 3 {
		t.Fatalf("expected video header, key frame and one G711 packet, got %d messages", sub.len())
	}

	wantMarkers := []byte{1, 2, base.RtmpSoundFormatG711A<<4 | 3}
	for i, want := range wantMarkers {
		if got := sub.markerAt(i); got != want {
			t.Fatalf("message %d marker = %d, want %d", i, got, want)
		}
	}
}

func TestAddConsumerWithReplayDisabledDoesNotReplayCachedGop(t *testing.T) {
	group := newTestGroup("test-no-replay")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-no-replay")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(videoKeyNalu(2))
	group.OnMsg(videoInterNalu(3))

	sub := &recordSubscriber{}
	group.AddConsumerWithReplay("consumer", sub, false)

	if sub.len() != 0 {
		t.Fatalf("expected no cached messages when replay is disabled, got %d messages", sub.len())
	}

	group.OnMsg(videoInterNalu(4))
	if sub.len() != 0 {
		t.Fatalf("expected to wait for next key frame, got %d messages", sub.len())
	}

	group.OnMsg(videoKeyNalu(5))
	if sub.len() != 2 {
		t.Fatalf("expected header and current key frame, got %d messages", sub.len())
	}
	if got := sub.markerAt(0); got != 1 {
		t.Fatalf("header marker = %d, want 1", got)
	}
	if got := sub.markerAt(1); got != 5 {
		t.Fatalf("key frame marker = %d, want 5", got)
	}
}

func TestAddConsumerReplayDoesNotInterleaveWithLiveKeyFrame(t *testing.T) {
	group := newTestGroup("test-replay-order")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-replay-order")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(videoKeyNalu(2))
	group.OnMsg(videoInterNalu(3))

	sub := newBlockingSubscriber()
	addDone := make(chan struct{})
	go func() {
		group.AddConsumer("consumer", sub)
		close(addDone)
	}()

	<-sub.blocked

	liveDone := make(chan struct{})
	go func() {
		group.OnMsg(videoKeyNalu(4))
		close(liveDone)
	}()

	select {
	case <-liveDone:
		t.Fatal("live key frame should not be delivered before cached GOP replay finishes")
	case <-time.After(50 * time.Millisecond):
	}

	close(sub.release)
	<-addDone
	<-liveDone

	wantMarkers := []byte{1, 2, 3, 4}
	gotMarkers := sub.markers()
	if len(gotMarkers) != len(wantMarkers) {
		t.Fatalf("markers = %v, want %v", gotMarkers, wantMarkers)
	}
	for i, want := range wantMarkers {
		if got := gotMarkers[i]; got != want {
			t.Fatalf("message %d marker = %d, want %d, all=%v", i, got, want, gotMarkers)
		}
	}
}

func TestSubscriberRemovingItselfStopsReplayDelivery(t *testing.T) {
	group := newTestGroup("test-self-remove-replay")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-self-remove-replay")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(videoKeyNalu(2))
	group.OnMsg(videoInterNalu(3))

	sub := &selfRemovingSubscriber{group: group, id: "consumer"}
	group.AddConsumer(sub.id, sub)

	if sub.len() != 1 {
		t.Fatalf("messages after self remove = %d, want 1", sub.len())
	}
	if got := sub.markerAt(0); got != 1 {
		t.Fatalf("first marker = %d, want 1", got)
	}

	group.OnMsg(videoKeyNalu(4))
	if sub.len() != 1 {
		t.Fatalf("messages after live frame = %d, want 1", sub.len())
	}
}

func TestSubscriberRemovingItselfStopsHeaderAndLiveDelivery(t *testing.T) {
	group := newTestGroup("test-self-remove-live")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-self-remove-live")

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(aacSeqHeader(2))

	sub := &selfRemovingSubscriber{group: group, id: "consumer"}
	group.AddConsumerWithReplay(sub.id, sub, false)

	group.OnMsg(videoKeyNalu(3))
	if sub.len() != 1 {
		t.Fatalf("messages after self remove = %d, want 1", sub.len())
	}
	if got := sub.markerAt(0); got != 1 {
		t.Fatalf("first marker = %d, want 1", got)
	}
}

func TestGroupManagerSupportsAppNameAndStreamName(t *testing.T) {
	manager := NewComplexGroupManager()
	group := &Group{key: NewStreamKey("live", "camera")}

	manager.setGroup(group.Key(), group)

	ok, got := manager.GetGroup(NewStreamKey("live", "camera"))
	if !ok || got != group {
		t.Fatal("expected exact appName and streamName lookup")
	}

	ok, got = manager.GetGroup(StreamKeyFromStreamName("camera"))
	if !ok || got != group {
		t.Fatal("expected streamName-only lookup to find the unique appName group")
	}
}

func TestGroupManagerStreamNameFallbackRejectsAmbiguousAppName(t *testing.T) {
	manager := NewComplexGroupManager()
	manager.setGroup(NewStreamKey("app1", "camera"), &Group{key: NewStreamKey("app1", "camera")})
	manager.setGroup(NewStreamKey("app2", "camera"), &Group{key: NewStreamKey("app2", "camera")})

	ok, got := manager.GetGroup(StreamKeyFromStreamName("camera"))
	if ok || got != nil {
		t.Fatal("expected ambiguous streamName-only lookup to fail")
	}
}

func TestGroupManagerGetOrCreateGroupReturnsExisting(t *testing.T) {
	manager := NewComplexGroupManager()
	key := NewStreamKey("live", "camera")

	group, created := manager.GetOrCreateGroup(key, "first", nil, 1, 0)
	if !created || group == nil {
		t.Fatal("expected group to be created")
	}

	got, created := manager.GetOrCreateGroup(key, "second", nil, 1, 0)
	if created || got != group {
		t.Fatal("expected existing group to be returned")
	}
	if got.UniqueKey() != "first" {
		t.Fatalf("unique key = %s, want first", got.UniqueKey())
	}
}

func TestGroupManagerGetOrCreateWaitsForClosedGroupCleanup(t *testing.T) {
	manager := NewComplexGroupManager()
	key := StreamKeyFromStreamName("camera")
	oldGroup := &Group{key: key}
	oldGroup.closed.Store(true)
	manager.setGroup(key, oldGroup)

	oldGroup.lifecycleMux.Lock()
	done := make(chan struct {
		group   *Group
		created bool
	})
	go func() {
		group, created := manager.GetOrCreateGroup(key, "new", nil, 1, 0)
		done <- struct {
			group   *Group
			created bool
		}{group: group, created: created}
	}()

	select {
	case <-done:
		t.Fatal("new group should wait for old group cleanup")
	case <-time.After(50 * time.Millisecond):
	}

	oldGroup.lifecycleMux.Unlock()

	select {
	case result := <-done:
		if !result.created || result.group == nil || result.group == oldGroup {
			t.Fatalf("unexpected group result: group=%p created=%v", result.group, result.created)
		}
	case <-time.After(time.Second):
		t.Fatal("new group was not created after old group cleanup")
	}
}

func TestGroupManagerGetOrCreateReturnsReplacementAfterWaitingClosedGroup(t *testing.T) {
	manager := NewComplexGroupManager()
	key := StreamKeyFromStreamName("camera")
	oldGroup := &Group{key: key}
	replacement := &Group{key: key}
	oldGroup.closed.Store(true)
	manager.setGroup(key, oldGroup)

	oldGroup.lifecycleMux.Lock()
	done := make(chan struct {
		group   *Group
		created bool
	})
	go func() {
		group, created := manager.GetOrCreateGroup(key, "new", nil, 1, 0)
		done <- struct {
			group   *Group
			created bool
		}{group: group, created: created}
	}()

	time.Sleep(50 * time.Millisecond)
	manager.setGroup(key, replacement)
	oldGroup.lifecycleMux.Unlock()

	select {
	case result := <-done:
		if result.created || result.group != replacement {
			t.Fatalf("unexpected group result: group=%p replacement=%p created=%v", result.group, replacement, result.created)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement group was not returned after old group cleanup")
	}
}

func TestGroupManagerRemoveGroupIfMatchDoesNotRemoveNewGroup(t *testing.T) {
	manager := NewComplexGroupManager()
	key := StreamKeyFromStreamName("camera")
	oldGroup := &Group{key: key}
	newGroup := &Group{key: key}

	manager.setGroup(key, oldGroup)
	manager.setGroup(key, newGroup)
	manager.RemoveGroupIfMatch(key, oldGroup)

	ok, got := manager.GetGroup(key)
	if !ok || got != newGroup {
		t.Fatal("old group stop should not remove new group")
	}
}

func TestGroupManagerIterateRemoveDoesNotRemoveReplacement(t *testing.T) {
	manager := NewComplexGroupManager()
	key := StreamKeyFromStreamName("camera")
	oldGroup := &Group{key: key}
	newGroup := &Group{key: key}

	manager.setGroup(key, oldGroup)
	manager.Iterate(func(iterKey StreamKey, group *Group) bool {
		if iterKey != key || group != oldGroup {
			t.Fatalf("unexpected iterate entry, key=%v group=%p", iterKey, group)
		}
		manager.setGroup(key, newGroup)
		return false
	})

	ok, got := manager.GetGroup(key)
	if !ok || got != newGroup {
		t.Fatal("iterate removal should not remove a replacement group")
	}
}

func TestGopCacheClearReleasesStaleGopPayloads(t *testing.T) {
	cache := NewGopCache(1, 0)

	cache.Feed(videoKeyNalu(1))
	cache.Feed(videoInterNalu(2))
	cache.Clear()

	if cache.GetGopCount() != 0 {
		t.Fatalf("gop count = %d, want 0", cache.GetGopCount())
	}
	for i, gop := range cache.data {
		if gop.data != nil {
			t.Fatalf("gop %d data was not released", i)
		}
	}
}

func TestGopCacheNegativeFrameLimitMeansUnlimited(t *testing.T) {
	cache := NewGopCache(1, -1)

	cache.Feed(videoKeyNalu(1))
	cache.Feed(videoInterNalu(2))

	msgs := cache.GetGopDataAt(0)
	if len(msgs) != 2 {
		t.Fatalf("cached messages = %d, want 2", len(msgs))
	}
}

func TestOnStopIsIdempotentAndClosesSubscribers(t *testing.T) {
	group := newTestGroup("test-stop")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-stop")

	sub := &recordSubscriber{}
	group.AddConsumer("consumer", sub)

	group.OnStop()
	group.OnStop()

	if sub.stopCountValue() != 1 {
		t.Fatalf("stop count = %d, want 1", sub.stopCountValue())
	}

	group.OnMsg(videoKeyNalu(1))
	if sub.len() != 0 {
		t.Fatalf("expected no messages after stop, got %d", sub.len())
	}
}

func TestOnMsgTriggersActiveHookOnceOnFirstMediaPacket(t *testing.T) {
	group := newTestGroup("test-active-hook")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-active-hook")

	key := StreamKeyFromStreamName("test-active-hook")
	var got []StreamKey
	group.BindActiveHook(key, func(k StreamKey) {
		got = append(got, k)
	})

	group.OnMsg(videoSeqHeader(1))
	group.OnMsg(aacSeqHeader(2))
	if len(got) != 0 {
		t.Fatalf("active hook count after seq header = %d, want 0", len(got))
	}
	group.OnMsg(videoKeyNalu(3))
	group.OnMsg(aacRaw(4))

	if len(got) != 1 {
		t.Fatalf("active hook count = %d, want 1", len(got))
	}
	if got[0] != key {
		t.Fatalf("active hook key = %+v, want %+v", got[0], key)
	}
}

func TestAddSubscriberAfterStopIsIgnored(t *testing.T) {
	group := newTestGroup("test-add-after-stop")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-add-after-stop")

	group.OnStop()

	sub := &recordSubscriber{}
	group.AddConsumer("consumer", sub)
	group.OnMsg(videoKeyNalu(1))

	if sub.len() != 0 {
		t.Fatalf("expected no messages after adding to stopped group, got %d", sub.len())
	}
	if len(group.StatSubscribers()) != 0 {
		t.Fatalf("expected no subscribers after adding to stopped group, got %d", len(group.StatSubscribers()))
	}
}

func TestDuplicateSubscriberIDIsIgnored(t *testing.T) {
	group := newTestGroup("test-duplicate")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-duplicate")

	first := &recordSubscriber{}
	second := &recordSubscriber{}
	group.AddConsumer("consumer", first)
	group.AddConsumer("consumer", second)

	group.OnMsg(videoKeyNalu(1))

	if first.len() != 1 {
		t.Fatalf("first subscriber messages = %d, want 1", first.len())
	}
	if second.len() != 0 {
		t.Fatalf("duplicate subscriber messages = %d, want 0", second.len())
	}
}

func TestStatSubscribersRefreshRuntimeStats(t *testing.T) {
	group := newTestGroup("test-stat-refresh")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-stat-refresh")

	sub := &statSubscriber{}
	sub.setStat(SubscriberStat{
		RemoteAddr:    "127.0.0.1:9000",
		ReadBytesSum:  1024,
		WroteBytesSum: 2048,
	})
	group.AddSubscriber(SubscriberInfo{
		SubscriberID: "stat-sub",
		Protocol:     SubscriberProtocolWHEP,
	}, sub)

	state := testSubscriberState(t, group, "stat-sub")
	state.UpdateStat(2)

	subs := group.StatSubscribers()
	if len(subs) != 1 {
		t.Fatalf("subscriber count = %d, want 1", len(subs))
	}

	stat := subs[0]
	if stat.RemoteAddr != "127.0.0.1:9000" {
		t.Fatalf("remote addr = %s, want 127.0.0.1:9000", stat.RemoteAddr)
	}
	if stat.ReadBytesSum != 1024 {
		t.Fatalf("read bytes = %d, want 1024", stat.ReadBytesSum)
	}
	if stat.WroteBytesSum != 2048 {
		t.Fatalf("wrote bytes = %d, want 2048", stat.WroteBytesSum)
	}
	if stat.ReadBitrateKbits != 4 {
		t.Fatalf("read bitrate = %d, want 4", stat.ReadBitrateKbits)
	}
	if stat.WriteBitrateKbits != 8 {
		t.Fatalf("write bitrate = %d, want 8", stat.WriteBitrateKbits)
	}
	if stat.BitrateKbits != 8 {
		t.Fatalf("bitrate = %d, want 8", stat.BitrateKbits)
	}

	sub.setStat(SubscriberStat{
		RemoteAddr:    "127.0.0.1:9001",
		ReadBytesSum:  1536,
		WroteBytesSum: 3072,
	})
	state.UpdateStat(1)

	subs = group.StatSubscribers()
	stat = subs[0]
	if stat.RemoteAddr != "127.0.0.1:9001" {
		t.Fatalf("remote addr = %s, want 127.0.0.1:9001", stat.RemoteAddr)
	}
	if stat.ReadBitrateKbits != 4 {
		t.Fatalf("read bitrate after increment = %d, want 4", stat.ReadBitrateKbits)
	}
	if stat.WriteBitrateKbits != 8 {
		t.Fatalf("write bitrate after increment = %d, want 8", stat.WriteBitrateKbits)
	}
}

type statPublisher struct {
	stat PublisherStat
}

func (p *statPublisher) GetPublisherStat() PublisherStat {
	return p.stat
}

func TestStatPublisherRefreshRuntimeStats(t *testing.T) {
	group := newTestGroup("test-publisher-stat")
	defer GetGroupManagerInstance().RemoveGroupByStreamName("test-publisher-stat")

	pub := &statPublisher{}
	pub.stat = PublisherStat{
		RemoteAddr:   "127.0.0.1:8000",
		ReadBytesSum: 4096,
	}
	group.AddPublisher(PublisherInfo{
		PublisherID: "CUSTOMIZEPUB1",
		Protocol:    PublisherProtocolWHIP,
	}, pub)

	stat := group.StatPublisher()
	if stat.SessionId != "CUSTOMIZEPUB1" {
		t.Fatalf("session id = %s, want CUSTOMIZEPUB1", stat.SessionId)
	}
	if stat.Protocol != PublisherProtocolWHIP {
		t.Fatalf("protocol = %s, want WHIP", stat.Protocol)
	}
	if stat.BaseType != base.SessionBaseTypePubStr {
		t.Fatalf("base type = %s, want PUB", stat.BaseType)
	}
	if stat.RemoteAddr != "127.0.0.1:8000" {
		t.Fatalf("remote addr = %s, want 127.0.0.1:8000", stat.RemoteAddr)
	}
	if stat.ReadBytesSum != 4096 {
		t.Fatalf("read bytes = %d, want 4096", stat.ReadBytesSum)
	}

	group.publisherMux.RLock()
	state := group.publisher
	group.publisherMux.RUnlock()
	state.refreshStat(2)

	pub.stat.ReadBytesSum = 8192
	sessionStat := state.refreshStat(2)
	if sessionStat.ReadBytesSum != 8192 {
		t.Fatalf("read bytes after increment = %d, want 8192", sessionStat.ReadBytesSum)
	}
	if sessionStat.ReadBitrateKbits != 16 {
		t.Fatalf("read bitrate = %d, want 16", sessionStat.ReadBitrateKbits)
	}
	if sessionStat.BitrateKbits != 16 {
		t.Fatalf("bitrate = %d, want 16", sessionStat.BitrateKbits)
	}

	group.RemovePublisher("CUSTOMIZEPUB1")
	stat = group.StatPublisher()
	if stat.SessionId != "" {
		t.Fatalf("expected empty publisher stat after remove, got %+v", stat)
	}
}
