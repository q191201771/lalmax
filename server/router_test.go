package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	maxlogic "github.com/q191201771/lalmax/logic"

	config "github.com/q191201771/lalmax/config"

	"github.com/q191201771/lal/pkg/base"
	baseLogic "github.com/q191201771/lal/pkg/logic"
)

type testHookPlugin struct {
	name   string
	events chan HookEvent
}

type maxlogicTestSubscriber struct {
	stat maxlogic.SubscriberStat
}

type hookHTTPPayload struct {
	SessionID  string `json:"session_id"`
	AppName    string `json:"app_name"`
	StreamName string `json:"stream_name"`
}

func (s *maxlogicTestSubscriber) OnMsg(msg base.RtmpMsg) {}

func (s *maxlogicTestSubscriber) OnStop() {}

func (s *maxlogicTestSubscriber) GetSubscriberStat() maxlogic.SubscriberStat {
	return s.stat
}

func (p *testHookPlugin) Name() string {
	return p.name
}

func (p *testHookPlugin) OnHookEvent(event HookEvent) error {
	p.events <- event
	return nil
}

var max *LalMaxServer
var onUpdateHook func(base.UpdateInfo)
var onUpdateHookMu sync.RWMutex
var testSeq atomic.Int64

const httpNotifyAddr = ":55559"

func uniqueTestName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, testSeq.Add(1))
}

func findTestGroup(groups []LalmaxStatGroup, streamName string) *LalmaxStatGroup {
	for i := range groups {
		if groups[i].StreamName == streamName {
			return &groups[i]
		}
	}
	return nil
}

func TestMain(m *testing.M) {
	var err error
	max, err = NewLalMaxServer(&config.Config{
		Fmp4Config: config.Fmp4Config{
			Http: config.Fmp4HttpConfig{Enable: true},
		},
		LalRawContent: []byte(`{"rtmp":{"enable":false},"rtsp":{"enable":false},"http_api":{"enable":false},"pprof":{"enable":false}}`),
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52349",
		},
		HttpNotifyConfig: config.HttpNotifyConfig{
			Enable:            true,
			UpdateIntervalSec: 2,
			OnUpdate:          fmt.Sprintf("http://127.0.0.1%s/on_update", httpNotifyAddr),
		},
	})
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/on_update", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var out base.UpdateInfo
		if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		onUpdateHookMu.RLock()
		hook := onUpdateHook
		onUpdateHookMu.RUnlock()
		if hook != nil {
			hook(out)
		}

		w.WriteHeader(http.StatusOK)
	})

	ln, err := net.Listen("tcp", httpNotifyAddr)
	if err != nil {
		panic(err)
	}
	go func() {
		_ = http.Serve(ln, nil)
	}()

	go max.Run()
	os.Exit(m.Run())
}

func TestAllGroup(t *testing.T) {
	streamName := uniqueTestName("test_all_group")
	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("no consumer", func(t *testing.T) {
		r := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/stat/all_group", nil)
		max.router.ServeHTTP(r, req)
		resp := r.Result()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
		var out ApiStatAllGroupResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		group := findTestGroup(out.Data.Groups, streamName)
		if group == nil {
			t.Fatal("no group")
		}
		if len(group.StatSubs) != 0 {
			t.Fatal("subs err")
		}
		if len(group.Lalmax.ExtSubs) != 0 {
			t.Fatal("lalmax ext_subs err")
		}
	})

	t.Run("has consumer", func(t *testing.T) {
		ss, _ := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(streamName, streamName, max.hlssvr, 1, 0)
		ss.AddConsumer("consumer1", nil)

		r := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/stat/all_group", nil)
		max.router.ServeHTTP(r, req)
		resp := r.Result()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
		var out ApiStatAllGroupResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		group := findTestGroup(out.Data.Groups, streamName)
		if group == nil {
			t.Fatal("no group")
		}
		if len(group.StatSubs) <= 0 {
			t.Fatal("subs err")
		}
		if len(group.Lalmax.ExtSubs) != 1 {
			t.Fatalf("unexpected lalmax ext_subs len: %d", len(group.Lalmax.ExtSubs))
		}
		if group.StatSubs[0].SessionId != "consumer1" {
			t.Fatal("SessionId err")
		}
		if group.Lalmax.ExtSubs[0].SessionId != "consumer1" {
			t.Fatal("lalmax ext SessionId err")
		}
	})
}

func TestNotifyUpdate(t *testing.T) {
	streamName := uniqueTestName("notify_test")
	consumerID := uniqueTestName("consumer_notify")
	matched := make(chan struct{}, 1)

	onUpdateHookMu.Lock()
	onUpdateHook = func(out base.UpdateInfo) {
		for _, group := range out.Groups {
			for _, sub := range group.StatSubs {
				if sub.SessionId == consumerID {
					select {
					case matched <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}
	onUpdateHookMu.Unlock()
	t.Cleanup(func() {
		onUpdateHookMu.Lock()
		onUpdateHook = nil
		onUpdateHookMu.Unlock()
	})

	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(streamName, streamName, max.hlssvr, 1, 0)
	ss.AddConsumer(consumerID, nil)

	select {
	case <-matched:
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive on_update with expected SessionId")
	}
}

func TestRtpPubStartStop(t *testing.T) {
	body := bytes.NewBufferString(`{"stream_name":"rtp_pub_test","port":0,"timeout_ms":0}`)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/ctrl/start_rtp_pub", body)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var startResp base.ApiCtrlStartRtpPubResp
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		t.Fatal(err)
	}
	if startResp.ErrorCode != base.ErrorCodeSucc {
		t.Fatalf("start_rtp_pub failed, code=%d desp=%s", startResp.ErrorCode, startResp.Desp)
	}
	if startResp.Data.StreamName != "rtp_pub_test" || startResp.Data.SessionId == "" || startResp.Data.Port == 0 {
		t.Fatalf("unexpected start_rtp_pub data: %+v", startResp.Data)
	}

	r = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/ctrl/stop_rtp_pub?stream_name=rtp_pub_test", nil)
	max.router.ServeHTTP(r, req)
	resp = r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var stopResp base.ApiCtrlStopRelayPullResp
	if err := json.NewDecoder(resp.Body).Decode(&stopResp); err != nil {
		t.Fatal(err)
	}
	if stopResp.ErrorCode != base.ErrorCodeSucc {
		t.Fatalf("stop_rtp_pub failed, code=%d desp=%s", stopResp.ErrorCode, stopResp.Desp)
	}
	if stopResp.Data.SessionId != startResp.Data.SessionId {
		t.Fatalf("stop_rtp_pub session id = %s, want %s", stopResp.Data.SessionId, startResp.Data.SessionId)
	}
}

func TestStatGroupWithAppName(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/stat/group?stream_name=test&app_name=missing", nil)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out ApiStatGroupResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ErrorCode != base.ErrorCodeGroupNotFound {
		t.Fatalf("unexpected error code: %+v", out)
	}
}

func TestStatGroupIncludesLalmaxExtSubs(t *testing.T) {
	streamName := uniqueTestName("test_stat_group_ext")

	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}

	ss, _ := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(streamName, streamName, max.hlssvr, 1, 0)
	ss.AddConsumer("consumer-stat-group", nil)

	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/stat/group?stream_name="+streamName, nil)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out ApiStatGroupResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ErrorCode != base.ErrorCodeSucc {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Data == nil {
		t.Fatal("group data is nil")
	}
	if len(out.Data.StatSubs) == 0 {
		t.Fatal("subs err")
	}
	if len(out.Data.Lalmax.ExtSubs) != 1 {
		t.Fatalf("unexpected lalmax ext_subs len: %d", len(out.Data.Lalmax.ExtSubs))
	}
	if out.Data.Lalmax.ExtSubs[0].SessionId != "consumer-stat-group" {
		t.Fatalf("unexpected ext sub: %+v", out.Data.Lalmax.ExtSubs[0])
	}
}

func TestStatGroupIncludesLalmaxExtSubsRuntimeFields(t *testing.T) {
	streamName := uniqueTestName("test_stat_group_runtime")

	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}

	ss, _ := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(streamName, streamName, max.hlssvr, 1, 0)
	sub := &maxlogicTestSubscriber{
		stat: maxlogic.SubscriberStat{
			RemoteAddr:    "10.0.0.1:9000",
			ReadBytesSum:  1024,
			WroteBytesSum: 2048,
		},
	}
	ss.AddSubscriber(maxlogic.SubscriberInfo{
		SubscriberID: "consumer-runtime",
		Protocol:     maxlogic.SubscriberProtocolSRT,
	}, sub)

	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/stat/group?stream_name="+streamName, nil)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out ApiStatGroupResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ErrorCode != base.ErrorCodeSucc {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.Data == nil {
		t.Fatal("group data is nil")
	}
	if len(out.Data.Lalmax.ExtSubs) != 1 {
		t.Fatalf("unexpected lalmax ext_subs len: %d", len(out.Data.Lalmax.ExtSubs))
	}

	stat := out.Data.Lalmax.ExtSubs[0]
	if stat.RemoteAddr != "10.0.0.1:9000" {
		t.Fatalf("remote addr = %s, want 10.0.0.1:9000", stat.RemoteAddr)
	}
	if stat.ReadBytesSum != 1024 || stat.WroteBytesSum != 2048 {
		t.Fatalf("unexpected bytes stat: %+v", stat)
	}
}

func TestStopRelayPullAllowsGet(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ctrl/stop_relay_pull?stream_name=missing", nil)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out base.ApiCtrlStopRelayPullResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ErrorCode != base.ErrorCodeGroupNotFound {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestHookHubRecentAndSubscribe(t *testing.T) {
	hub := NewHttpNotify(config.HttpNotifyConfig{}, "hub-test")
	// NotifyPubStart 会派生 on_stream_changed，需要足够缓冲
	_, ch, cancel := hub.Subscribe(8)
	defer cancel()

	hub.NotifyPubStart(base.PubStartInfo{})

	select {
	case event := <-ch:
		if event.Event != HookEventPubStart {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("wait hook event timeout")
	}

	events := hub.Recent(0)
	found := false
	for _, e := range events {
		if e.Event == HookEventPubStart {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("on_pub_start not found in recent events")
	}
}

func TestHookGroupEventsFromDirectLifecycle(t *testing.T) {
	svr, err := NewLalMaxServer(&config.Config{
		LalRawContent: []byte(`{"rtmp":{"enable":false},"rtsp":{"enable":false},"http_api":{"enable":false},"pprof":{"enable":false}}`),
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52353",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	svr.lalsvr.WithOnHookSession(func(uniqueKey string, streamName string) baseLogic.ICustomizeHookSessionContext {
		key := maxlogic.StreamKeyFromStreamName(streamName)
		group, created := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(uniqueKey, streamName, svr.hlssvr, svr.conf.LogicConfig.GopCacheNum, svr.conf.LogicConfig.SingleGopMaxFrameNum)
		group.BindStopHook(key, func(stopKey maxlogic.StreamKey) {
			svr.notifyHub.NotifyGroupStop(HookGroupInfo{
				AppName:    stopKey.AppName,
				StreamName: stopKey.StreamName,
			})
		})
		if created {
			svr.notifyHub.NotifyGroupStart(HookGroupInfo{
				AppName:    key.AppName,
				StreamName: key.StreamName,
			})
		}
		return group
	})

	streamName := "direct-group-lifecycle"
	session, err := svr.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}
	svr.lalsvr.DelCustomizePubSession(session)

	filter := NewHookEventFilter("", streamName, "", []string{HookEventGroupStart, HookEventGroupStop})
	events := svr.notifyHub.RecentFiltered(10, filter)
	if len(events) != 2 {
		t.Fatalf("unexpected event len: %d", len(events))
	}
	if events[0].Event != HookEventGroupStart {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Event != HookEventGroupStop {
		t.Fatalf("unexpected second event: %+v", events[1])
	}

	var start HookGroupInfo
	if err := json.Unmarshal(events[0].Payload, &start); err != nil {
		t.Fatal(err)
	}
	if start.StreamName != streamName {
		t.Fatalf("unexpected start payload: %+v", start)
	}

	var stop HookGroupInfo
	if err := json.Unmarshal(events[1].Payload, &stop); err != nil {
		t.Fatal(err)
	}
	if stop.StreamName != streamName {
		t.Fatalf("unexpected stop payload: %+v", stop)
	}
}

func TestHookHubStreamActiveEvent(t *testing.T) {
	hub := NewHttpNotify(config.HttpNotifyConfig{}, "hub-test")

	hub.NotifyStreamActive(HookGroupInfo{
		AppName:    "live",
		StreamName: "stream-active",
	})

	filter := NewHookEventFilter("live", "stream-active", "", []string{HookEventStreamActive})
	events := hub.RecentFiltered(10, filter)
	if len(events) != 1 {
		t.Fatalf("unexpected event len: %d", len(events))
	}
	if events[0].Event != HookEventStreamActive {
		t.Fatalf("unexpected event: %+v", events[0])
	}

	var payload HookGroupInfo
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AppName != "live" || payload.StreamName != "stream-active" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuiltinHTTPPluginRespectsEnableFlag(t *testing.T) {
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:     false,
		OnPubStart: ts.URL,
	}, "hub-test")

	hub.NotifyPubStart(base.PubStartInfo{})
	time.Sleep(200 * time.Millisecond)

	if got := requestCount.Load(); got != 0 {
		t.Fatalf("unexpected webhook request count: %d", got)
	}
}

func TestBuiltinHTTPPluginPreservesOrderPerStream(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	allowFirstFinish := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload hookHTTPPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch payload.SessionID {
		case "first":
			close(firstStarted)
			<-allowFirstFinish
		case "second":
			close(secondStarted)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:     true,
		OnPubStart: ts.URL,
		OnPubStop:  ts.URL,
	}, "hub-test")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "first",
			AppName:    "live",
			StreamName: "same-stream",
		},
	})
	hub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "second",
			AppName:    "live",
			StreamName: "same-stream",
		},
	})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first webhook did not start in time")
	}

	select {
	case <-secondStarted:
		t.Fatal("second webhook started before the first one finished")
	case <-time.After(200 * time.Millisecond):
	}

	close(allowFirstFinish)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second webhook did not start after the first one finished")
	}
}

func TestBuiltinHTTPPluginAllowsParallelAcrossStreams(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStreamStarted := make(chan struct{})
	allowFirstFinish := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload hookHTTPPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch payload.StreamName {
		case "stream-a":
			close(firstStarted)
			<-allowFirstFinish
		case "stream-b":
			close(secondStreamStarted)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:     true,
		OnPubStart: ts.URL,
	}, "hub-test")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "stream-a-session",
			AppName:    "live",
			StreamName: "stream-a",
		},
	})
	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "stream-b-session",
			AppName:    "live",
			StreamName: "stream-b",
		},
	})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first stream webhook did not start in time")
	}

	select {
	case <-secondStreamStarted:
	case <-time.After(time.Second):
		t.Fatal("second stream webhook was blocked by the first stream")
	}

	close(allowFirstFinish)
}

func TestBuiltinHTTPPluginPreservesOrderAcrossDifferentURLsForSameStream(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	allowFirstFinish := make(chan struct{})

	startTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload hookHTTPPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode start payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		close(firstStarted)
		<-allowFirstFinish
		w.WriteHeader(http.StatusOK)
	}))
	defer startTS.Close()

	stopTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var payload hookHTTPPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode stop payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		close(secondStarted)
		w.WriteHeader(http.StatusOK)
	}))
	defer stopTS.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:     true,
		OnPubStart: startTS.URL,
		OnPubStop:  stopTS.URL,
	}, "hub-test")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "first",
			AppName:    "live",
			StreamName: "same-stream",
		},
	})
	hub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "second",
			AppName:    "live",
			StreamName: "same-stream",
		},
	})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("start webhook did not start in time")
	}

	select {
	case <-secondStarted:
		t.Fatal("stop webhook started before start webhook finished")
	case <-time.After(200 * time.Millisecond):
	}

	close(allowFirstFinish)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("stop webhook did not start after start webhook finished")
	}
}

func TestHookRecentEndpoint(t *testing.T) {
	svr, err := NewLalMaxServer(&config.Config{
		LalRawContent: []byte(`{"rtmp":{"enable":false},"rtsp":{"enable":false},"http_api":{"enable":false},"pprof":{"enable":false}}`),
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52350",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	svr.notifyHub.NotifyPubStop(base.PubStopInfo{})

	r := httptest.NewRecorder()
	// 用 event filter 精确查询，因为 NotifyPubStop 会派生 on_stream_changed
	req := httptest.NewRequest("GET", "/api/hook/recent?limit=10&event=on_pub_stop", nil)
	svr.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out struct {
		base.ApiRespBasic
		Data struct {
			Events []HookEvent `json:"events"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ErrorCode != base.ErrorCodeSucc {
		t.Fatalf("unexpected response: %+v", out)
	}
	if len(out.Data.Events) < 1 {
		t.Fatalf("expected at least 1 on_pub_stop event, got: %d", len(out.Data.Events))
	}
	if out.Data.Events[0].Event != HookEventPubStop {
		t.Fatalf("unexpected event: %+v", out.Data.Events[0])
	}
}

func TestHookRecentEndpointFilterByEventAndStream(t *testing.T) {
	svr, err := NewLalMaxServer(&config.Config{
		LalRawContent: []byte(`{"rtmp":{"enable":false},"rtsp":{"enable":false},"http_api":{"enable":false},"pprof":{"enable":false}}`),
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52351",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	svr.notifyHub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-1",
			StreamName: "stream-a",
			AppName:    "live",
		},
	})
	svr.notifyHub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-2",
			StreamName: "stream-b",
			AppName:    "live",
		},
	})

	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hook/recent?limit=10&stream_name=stream-a&event=on_pub_start", nil)
	svr.router.ServeHTTP(r, req)
	resp := r.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}

	var out struct {
		base.ApiRespBasic
		Data struct {
			Events []HookEvent `json:"events"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data.Events) != 1 {
		t.Fatalf("unexpected event count: %d", len(out.Data.Events))
	}
	if out.Data.Events[0].Event != HookEventPubStart {
		t.Fatalf("unexpected event: %+v", out.Data.Events[0])
	}
}

func TestHookEventFilterBySessionID(t *testing.T) {
	filter := NewHookEventFilter("", "", "sess-2", nil)

	pubStart := HookEvent{Event: HookEventPubStart, sessionID: "sess-1"}
	pubStop := HookEvent{Event: HookEventPubStop, sessionID: "sess-2"}

	if filter.Match(pubStart) {
		t.Fatalf("session filter unexpectedly matched: %+v", pubStart)
	}
	if !filter.Match(pubStop) {
		t.Fatalf("session filter did not match: %+v", pubStop)
	}
}

func TestHookEventFilterByUpdateGroup(t *testing.T) {
	filter := NewHookEventFilter("live", "stream-a", "", []string{HookEventUpdate})
	event := HookEvent{
		Event: HookEventUpdate,
		groupKeys: []maxlogic.StreamKey{
			maxlogic.NewStreamKey("live", "stream-a"),
			maxlogic.NewStreamKey("live", "stream-b"),
		},
	}

	if !filter.Match(event) {
		t.Fatalf("update filter did not match: %+v", event)
	}
}

func TestHookEventFilterByGroupLifecycle(t *testing.T) {
	filter := NewHookEventFilter("live", "stream-a", "", []string{HookEventGroupStart})
	event := HookEvent{
		Event:      HookEventGroupStart,
		appName:    "live",
		streamName: "stream-a",
	}

	if !filter.Match(event) {
		t.Fatalf("group lifecycle filter did not match: %+v", event)
	}
}

func TestHookPluginReceivesFilteredEvents(t *testing.T) {
	hub := NewHttpNotify(config.HttpNotifyConfig{}, "plugin-test")
	plugin := &testHookPlugin{
		name:   "stream-a-plugin",
		events: make(chan HookEvent, 2),
	}

	cancel, err := hub.RegisterPlugin(plugin, HookPluginOptions{
		Filter: NewHookEventFilter("live", "stream-a", "", []string{HookEventPubStart}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-a",
			StreamName: "stream-a",
			AppName:    "live",
		},
	})
	hub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-a",
			StreamName: "stream-a",
			AppName:    "live",
		},
	})

	select {
	case event := <-plugin.events:
		if event.Event != HookEventPubStart {
			t.Fatalf("unexpected plugin event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("wait plugin event timeout")
	}

	select {
	case event := <-plugin.events:
		t.Fatalf("unexpected extra plugin event: %+v", event)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRegisterHookPluginFromServer(t *testing.T) {
	svr, err := NewLalMaxServer(&config.Config{
		LalRawContent: []byte(`{"rtmp":{"enable":false},"rtsp":{"enable":false},"http_api":{"enable":false},"pprof":{"enable":false}}`),
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52352",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	plugin := &testHookPlugin{
		name:   "server-plugin",
		events: make(chan HookEvent, 1),
	}
	cancel, err := svr.RegisterHookPlugin(plugin, HookPluginOptions{
		Filter: NewHookEventFilter("", "", "", []string{HookEventPubStop}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	svr.notifyHub.NotifyPubStop(base.PubStopInfo{})

	select {
	case event := <-plugin.events:
		if event.Event != HookEventPubStop {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("wait server plugin event timeout")
	}
}

func TestAuthentication(t *testing.T) {
	t.Run("无须鉴权", func(t *testing.T) {
		if !authentication("12", "192.168.0.2", nil, nil) {
			t.Fatal("期望通过， 但实际未通过")
		}
	})
	t.Run("Token 鉴权失败", func(t *testing.T) {
		if authentication("1", "192.168.0.2", []string{"12"}, nil) {
			t.Fatal("期望不通过， 但实际通过")
		}
	})
	t.Run("token 鉴权成功", func(t *testing.T) {
		if !authentication("12", "192.168.0.2", []string{"12"}, nil) {
			t.Fatal("期望通过， 但实际不通过")
		}
	})
	t.Run("ip 白名单鉴权失败", func(t *testing.T) {
		if authentication("12", "192.168.0.2", nil, []string{"192.168.1.2"}) {
			t.Fatal("期望不通过， 但实际通过")
		}
	})
	t.Run("ip 白名单鉴权成功", func(t *testing.T) {
		if !authentication("12", "192.168.0.2", []string{"12"}, []string{"192.168.0.2"}) {
			t.Fatal("期望通过， 但实际不通过")
		}
	})
	t.Run("两种模式结合鉴权通过", func(t *testing.T) {
		if !authentication("12", "192.168.0.2", []string{"12"}, []string{"192.168.0.2"}) {
			t.Fatal("期望通过， 但实际不通过")
		}
	})
}

// TestWHIPGETNot404 确保浏览器 GET 能命中路由（无 RTC 时为 503，不应为 Gin 默认 404）。
func TestWHIPGETNot404(t *testing.T) {
	paths := []string{"/webrtc/whip?streamid=test110"}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			r := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			max.router.ServeHTTP(r, req)
			resp := r.Result()
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf("GET %s 不应返回 404，请检查 initRtcRouter 是否注册 GET", p)
			}
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("测试环境未启用 RTC，期望 503，实际 %d", resp.StatusCode)
			}
		})
	}
}

// TestWHEPGETCanonicalPath 规范播放地址 GET /webrtc/whep 应命中路由（无 RTC 时为 503）。
func TestWHEPGETCanonicalPath(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webrtc/whep?streamid=test110", nil)
	max.router.ServeHTTP(r, req)
	resp := r.Result()
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("GET /webrtc/whep 不应 404")
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("测试环境未启用 RTC，期望 503，实际 %d", resp.StatusCode)
	}
}
