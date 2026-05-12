package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	config "github.com/q191201771/lalmax/config"

	"github.com/q191201771/lal/pkg/base"
)

// ===========================================================================
// REST API 兼容测试
// ===========================================================================

func TestZlmCompatOpenRtpServer(t *testing.T) {
	body := `{"port":0,"tcp_mode":0,"stream_id":"zlm_compat_rtp_test"}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/openRtpServer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d, body: %s", r.Code, r.Body.String())
	}

	var resp ZlmOpenRtpServerResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Port == 0 {
		t.Fatal("expected non-zero port")
	}

	// 清理：关闭刚开启的 RTP 服务
	t.Cleanup(func() {
		closeBody := `{"stream_id":"zlm_compat_rtp_test"}`
		cr := httptest.NewRecorder()
		creq := httptest.NewRequest("POST", "/index/api/closeRtpServer", strings.NewReader(closeBody))
		creq.Header.Set("Content-Type", "application/json")
		max.router.ServeHTTP(cr, creq)
	})
}

func TestZlmCompatCloseRtpServer(t *testing.T) {
	// 先开启
	openBody := `{"port":0,"tcp_mode":0,"stream_id":"zlm_close_rtp_test"}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/openRtpServer", strings.NewReader(openBody))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("open failed: %d %s", r.Code, r.Body.String())
	}

	// 再关闭
	closeBody := `{"stream_id":"zlm_close_rtp_test"}`
	r = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/index/api/closeRtpServer", strings.NewReader(closeBody))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("close failed: %d %s", r.Code, r.Body.String())
	}

	var resp ZlmCloseRtpServerResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code=0, got %d", resp.Code)
	}
	if resp.Hit != 1 {
		t.Fatalf("expected hit=1, got %d", resp.Hit)
	}
}

func TestZlmCompatCloseRtpServerNotFound(t *testing.T) {
	body := `{"stream_id":"nonexistent_stream_id"}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/closeRtpServer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", r.Code)
	}

	var resp ZlmCloseRtpServerResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Hit != 0 {
		t.Fatalf("expected hit=0 for nonexistent stream, got %d", resp.Hit)
	}
}

func TestZlmCompatCloseStreams(t *testing.T) {
	streamName := uniqueTestName("zlm_close_stream")
	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"app":"","stream":"` + streamName + `"}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/close_streams", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", r.Code, r.Body.String())
	}

	var resp ZlmCloseStreamsResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code=0, got %d", resp.Code)
	}
	if resp.CountHit == 0 {
		t.Fatal("expected count_hit > 0")
	}
}

func TestZlmCompatGetServerConfig(t *testing.T) {
	body := `{}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/getServerConfig", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", r.Code, r.Body.String())
	}

	var resp ZlmGetServerConfigResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code=0, got %d", resp.Code)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected non-empty data array")
	}

	// 验证返回的配置包含 ZLM 标准字段
	cfg := resp.Data[0]
	requiredKeys := []string{
		"http.port",
		"rtmp.port",
		"rtsp.port",
		"rtp_proxy.port",
		"general.mediaServerId",
		"hook.on_stream_changed",
	}
	for _, key := range requiredKeys {
		if _, ok := cfg[key]; !ok {
			t.Errorf("missing required config key: %s", key)
		}
	}
}

func TestZlmCompatSetServerConfig(t *testing.T) {
	body := `{
		"hook.on_stream_changed":"http://127.0.0.1:15123/webhook/on_stream_changed",
		"hook.on_server_keepalive":"http://127.0.0.1:15123/webhook/on_server_keepalive",
		"hook.on_publish":"http://127.0.0.1:15123/webhook/on_publish",
		"hook.on_play":"http://127.0.0.1:15123/webhook/on_play",
		"hook.on_stream_not_found":"http://127.0.0.1:15123/webhook/on_stream_not_found",
		"hook.on_stream_none_reader":"http://127.0.0.1:15123/webhook/on_stream_none_reader",
		"hook.on_record_mp4":"http://127.0.0.1:15123/webhook/on_record_mp4",
		"hook.on_server_started":"http://127.0.0.1:15123/webhook/on_server_started",
		"hook.alive_interval":"10"
	}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/setServerConfig", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", r.Code, r.Body.String())
	}

	var resp ZlmSetServerConfigResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code=0, got %d", resp.Code)
	}
	if resp.Changed < 8 {
		t.Fatalf("expected at least 8 changed, got %d", resp.Changed)
	}

	// 验证 getServerConfig 返回更新后的值
	r2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/index/api/getServerConfig", strings.NewReader(`{}`))
	req2.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r2, req2)

	var getResp ZlmGetServerConfigResp
	json.NewDecoder(r2.Body).Decode(&getResp)
	cfg := getResp.Data[0]

	if cfg["hook.on_stream_changed"] != "http://127.0.0.1:15123/webhook/on_stream_changed" {
		t.Errorf("on_stream_changed not updated: %v", cfg["hook.on_stream_changed"])
	}
	if cfg["hook.on_publish"] != "http://127.0.0.1:15123/webhook/on_publish" {
		t.Errorf("on_publish not updated: %v", cfg["hook.on_publish"])
	}
}

func TestZlmCompatAddStreamProxy(t *testing.T) {
	body := `{
		"vhost":"__defaultVhost__",
		"app":"live",
		"stream":"proxy_test",
		"url":"rtmp://127.0.0.1:19350/live/test",
		"retry_count":0,
		"rtp_type":0,
		"timeout_sec":5
	}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/addStreamProxy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", r.Code, r.Body.String())
	}

	var resp ZlmAddStreamProxyResp
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// 拉流可能因目标不存在而失败，但响应格式必须正确
	// code=0 表示成功，其他值表示拉流失败但格式正确
	if resp.Code == 0 && resp.Data.Key == "" {
		t.Fatal("code=0 but key is empty")
	}
}

func TestZlmCompatStartStopRecord(t *testing.T) {
	streamName := uniqueTestName("zlm_record")
	_, err := max.lalsvr.AddCustomizePubSession(streamName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// 清理：尝试停止录制
		stopBody := `{"type":1,"vhost":"__defaultVhost__","app":"live","stream":"` + streamName + `"}`
		sr := httptest.NewRecorder()
		sreq := httptest.NewRequest("POST", "/index/api/stopRecord", strings.NewReader(stopBody))
		sreq.Header.Set("Content-Type", "application/json")
		max.router.ServeHTTP(sr, sreq)
	}()

	// 开始录制
	startBody := `{"type":1,"vhost":"__defaultVhost__","app":"live","stream":"` + streamName + `"}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/index/api/startRecord", strings.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("start record unexpected status: %d %s", r.Code, r.Body.String())
	}

	var startResp ZlmStartRecordResp
	if err := json.NewDecoder(r.Body).Decode(&startResp); err != nil {
		t.Fatal(err)
	}
	if startResp.Code != 0 {
		t.Fatalf("start record expected code=0, got %d msg=%s", startResp.Code, startResp.Msg)
	}
	if !startResp.Result {
		t.Fatal("start record expected result=true")
	}

	// 停止录制
	stopBody := `{"type":1,"vhost":"__defaultVhost__","app":"live","stream":"` + streamName + `"}`
	r = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/index/api/stopRecord", strings.NewReader(stopBody))
	req.Header.Set("Content-Type", "application/json")
	max.router.ServeHTTP(r, req)

	if r.Code != http.StatusOK {
		t.Fatalf("stop record unexpected status: %d %s", r.Code, r.Body.String())
	}

	var stopResp ZlmStopRecordResp
	if err := json.NewDecoder(r.Body).Decode(&stopResp); err != nil {
		t.Fatal(err)
	}
	if stopResp.Code != 0 {
		t.Fatalf("stop record expected code=0, got %d msg=%s", stopResp.Code, stopResp.Msg)
	}
}

// ===========================================================================
// Hook 兼容测试
// ===========================================================================

// TestZlmHookOnStreamChangedFormat 验证 on_stream_changed hook 的 payload 格式与 ZLM 兼容
func TestZlmHookOnStreamChangedFormat(t *testing.T) {
	received := make(chan ZlmOnStreamChangedPayload, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload ZlmOnStreamChangedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode on_stream_changed payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:                true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamChanged: ts.URL},
	}, "zlm-hook-test")

	streamName := uniqueTestName("stream_changed_test")

	// 模拟推流开始 -> 应触发 on_stream_changed(regist=true)
	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-session-1",
			AppName:    "live",
			StreamName: streamName,
		},
	})

	select {
	case payload := <-received:
		if !payload.Regist {
			t.Fatal("expected regist=true on pub_start")
		}
		// gb28181 优先读 app_name/stream_name（lalmax 兼容字段）
		if payload.StreamName == "" && payload.Stream == "" {
			t.Fatal("expected stream or stream_name to be set")
		}
		if payload.AppName == "" && payload.App == "" {
			t.Fatal("expected app or app_name to be set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_stream_changed for pub_start")
	}

	// 模拟推流结束 -> 应触发 on_stream_changed(regist=false)
	hub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "pub-session-1",
			AppName:    "live",
			StreamName: streamName,
		},
	})

	select {
	case payload := <-received:
		if payload.Regist {
			t.Fatal("expected regist=false on pub_stop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_stream_changed for pub_stop")
	}
}

// TestZlmHookOnStreamChangedFieldCompleteness 验证 payload 包含 ZLM 必需字段
func TestZlmHookOnStreamChangedFieldCompleteness(t *testing.T) {
	received := make(chan json.RawMessage, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- raw
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamChanged: ts.URL},
	}, "field-test")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "completeness-sess",
			AppName:    "live",
			StreamName: "completeness-stream",
		},
	})

	select {
	case raw := <-received:
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}

		// ZLM on_stream_changed 必须包含的字段
		requiredFields := []string{
			"regist",
			"schema",
			"mediaServerId",
			"vhost",
		}
		for _, field := range requiredFields {
			if _, ok := m[field]; !ok {
				t.Errorf("missing required field in on_stream_changed: %s", field)
			}
		}

		// 必须有 app+stream 或 app_name+stream_name
		hasZlmStyle := m["app"] != nil && m["stream"] != nil
		hasLalmaxStyle := m["app_name"] != nil && m["stream_name"] != nil
		if !hasZlmStyle && !hasLalmaxStyle {
			t.Error("payload must contain (app, stream) or (app_name, stream_name)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_stream_changed")
	}
}

// TestZlmHookOnServerKeepalive 验证 keepalive hook 的触发和 payload 格式
func TestZlmHookOnServerKeepalive(t *testing.T) {
	received := make(chan ZlmOnServerKeepalivePayload, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload ZlmOnServerKeepalivePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode keepalive payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:               true,
		KeepaliveIntervalSec: 1,
		ZlmCompatHookConfig:  config.ZlmCompatHookConfig{ZlmOnServerKeepalive: ts.URL},
	}, "keepalive-test")

	// 手动触发 keepalive
	hub.NotifyServerKeepalive()

	select {
	case payload := <-received:
		if payload.MediaServerID == "" {
			t.Fatal("expected non-empty mediaServerId")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_server_keepalive")
	}
}

// TestZlmHookOnStreamNoneReader 验证无人观看 hook
func TestZlmHookOnStreamNoneReader(t *testing.T) {
	received := make(chan ZlmOnStreamNoneReaderPayload, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload ZlmOnStreamNoneReaderPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode none_reader payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamNoneReader: ts.URL},
	}, "none-reader-test")

	hub.NotifyStreamNoneReader(ZlmOnStreamNoneReaderPayload{
		MediaServerID: "none-reader-test",
		App:           "live",
		Schema:        "rtmp",
		Stream:        "test-stream",
		Vhost:         "__defaultVhost__",
	})

	select {
	case payload := <-received:
		if payload.App != "live" {
			t.Fatalf("expected app=live, got %s", payload.App)
		}
		if payload.Stream != "test-stream" {
			t.Fatalf("expected stream=test-stream, got %s", payload.Stream)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_stream_none_reader")
	}
}

// TestZlmHookOnRtpServerTimeout 验证 RTP 超时 hook
func TestZlmHookOnRtpServerTimeout(t *testing.T) {
	received := make(chan ZlmOnRtpServerTimeoutPayload, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload ZlmOnRtpServerTimeoutPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode rtp_timeout payload failed: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnRtpServerTimeout: ts.URL},
	}, "rtp-timeout-test")

	hub.NotifyRtpServerTimeout(ZlmOnRtpServerTimeoutPayload{
		LocalPort:     30000,
		StreamID:      "timeout_stream",
		TCPMode:       0,
		MediaServerID: "rtp-timeout-test",
	})

	select {
	case payload := <-received:
		if payload.StreamID != "timeout_stream" {
			t.Fatalf("expected stream_id=timeout_stream, got %s", payload.StreamID)
		}
		if payload.LocalPort != 30000 {
			t.Fatalf("expected local_port=30000, got %d", payload.LocalPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive on_rtp_server_timeout")
	}
}

// TestZlmHookOnStreamChangedOrderPerStream 验证同一流的 stream_changed 事件保序
func TestZlmHookOnStreamChangedOrderPerStream(t *testing.T) {
	var order atomic.Int32
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	allowFirst := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload ZlmOnStreamChangedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		seq := order.Add(1)
		if seq == 1 {
			close(firstDone)
			<-allowFirst
		} else if seq == 2 {
			close(secondDone)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamChanged: ts.URL},
	}, "order-test")

	streamName := uniqueTestName("order_stream")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "order-1",
			AppName:    "live",
			StreamName: streamName,
		},
	})
	hub.NotifyPubStop(base.PubStopInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			SessionId:  "order-2",
			AppName:    "live",
			StreamName: streamName,
		},
	})

	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first on_stream_changed not received")
	}

	// 第二个应被阻塞（同流保序）
	select {
	case <-secondDone:
		t.Fatal("second on_stream_changed should be blocked")
	case <-time.After(200 * time.Millisecond):
	}

	close(allowFirst)

	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second on_stream_changed not received after first finished")
	}
}

// ---------- on_publish ----------

func TestZlmHookOnPublish(t *testing.T) {
	received := make(chan map[string]any, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		received <- m
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnPublish: ts.URL},
	}, "pub-hook-test")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			AppName:    "live",
			StreamName: "test_pub",
			Protocol:   "rtmp",
		},
	})

	select {
	case m := <-received:
		if m["app"] != "live" || m["stream"] != "test_pub" || m["schema"] != "rtmp" {
			t.Fatalf("unexpected on_publish payload: %+v", m)
		}
		if m["mediaServerId"] != "pub-hook-test" {
			t.Fatalf("unexpected mediaServerId: %v", m["mediaServerId"])
		}
	case <-time.After(time.Second):
		t.Fatal("on_publish not received")
	}
}

// ---------- on_play ----------

func TestZlmHookOnPlay(t *testing.T) {
	received := make(chan map[string]any, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		received <- m
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnPlay: ts.URL},
	}, "play-hook-test")

	hub.NotifySubStart(base.SubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			AppName:    "live",
			StreamName: "test_play",
			Protocol:   "rtsp",
		},
	})

	select {
	case m := <-received:
		if m["app"] != "live" || m["stream"] != "test_play" || m["schema"] != "rtsp" {
			t.Fatalf("unexpected on_play payload: %+v", m)
		}
		if m["mediaServerId"] != "play-hook-test" {
			t.Fatalf("unexpected mediaServerId: %v", m["mediaServerId"])
		}
	case <-time.After(time.Second):
		t.Fatal("on_play not received")
	}
}

// ---------- on_stream_not_found ----------

func TestZlmHookOnStreamNotFound(t *testing.T) {
	received := make(chan map[string]any, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		received <- m
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamNotFound: ts.URL},
	}, "notfound-hook-test")

	hub.NotifyStreamNotFound(ZlmOnStreamNotFoundPayload{
		App:    "live",
		Stream: "missing_stream",
		Schema: "rtmp",
		Vhost:  "__defaultVhost__",
	})

	select {
	case m := <-received:
		if m["app"] != "live" || m["stream"] != "missing_stream" {
			t.Fatalf("unexpected on_stream_not_found payload: %+v", m)
		}
		if m["mediaServerId"] != "notfound-hook-test" {
			t.Fatalf("unexpected mediaServerId: %v", m["mediaServerId"])
		}
	case <-time.After(time.Second):
		t.Fatal("on_stream_not_found not received")
	}
}

// ---------- 融合兼容逻辑 ----------

func TestZlmHookDispatchByConfig(t *testing.T) {
	// 验证：配置了 ZLM hook URL → ZLM 回调触发；
	//       未配置 ZLM hook URL → ZLM 回调不触发；
	//       lalmax 原有回调始终按 URL 配置分发

	zlmReceived := make(chan string, 8)
	tZlm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		if _, ok := m["regist"]; ok {
			zlmReceived <- "on_stream_changed"
		} else {
			zlmReceived <- "on_publish"
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer tZlm.Close()

	lalReceived := make(chan string, 8)
	tLal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lalReceived <- "on_pub_start"
		w.WriteHeader(http.StatusOK)
	}))
	defer tLal.Close()

	// 同时配置 ZLM + lalmax → 两者都应触发
	hub := NewHttpNotify(config.HttpNotifyConfig{
		Enable:              true,
		OnPubStart:          tLal.URL,
		ZlmCompatHookConfig: config.ZlmCompatHookConfig{ZlmOnStreamChanged: tZlm.URL, ZlmOnPublish: tZlm.URL},
	}, "both-mode")

	hub.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			AppName:    "live",
			StreamName: "both_test",
			Protocol:   "rtmp",
		},
	})

	// ZLM 回调应触发（on_publish + on_stream_changed）
	for i := 0; i < 2; i++ {
		select {
		case evt := <-zlmReceived:
			t.Logf("both mode zlm: %s", evt)
		case <-time.After(time.Second):
			t.Fatal("both mode: expected zlm callback")
		}
	}

	// lalmax 原有回调也应触发
	select {
	case evt := <-lalReceived:
		t.Logf("both mode lal: %s", evt)
	case <-time.After(time.Second):
		t.Fatal("both mode: expected lalmax callback")
	}

	// 仅配置 lalmax，不配置 ZLM → ZLM 回调不应触发
	hubLal := NewHttpNotify(config.HttpNotifyConfig{
		Enable:     true,
		OnPubStart: tLal.URL,
	}, "lal-only")

	hubLal.NotifyPubStart(base.PubStartInfo{
		SessionEventCommonInfo: base.SessionEventCommonInfo{
			AppName:    "live",
			StreamName: "lal_only_test",
			Protocol:   "rtmp",
		},
	})

	select {
	case evt := <-lalReceived:
		t.Logf("lal-only mode: %s", evt)
	case <-time.After(time.Second):
		t.Fatal("lal-only mode: expected lalmax callback")
	}

	// ZLM 回调不应触发
	select {
	case <-zlmReceived:
		t.Fatal("lal-only mode: should NOT receive zlm callback")
	case <-time.After(200 * time.Millisecond):
	}
}
