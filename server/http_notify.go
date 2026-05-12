// Copyright 2020, Chef.  All rights reserved.
// https://github.com/q191201771/lal
//
// Use of this source code is governed by a MIT-style license
// that can be found in the License file.
//
// Author: Chef (191201771@qq.com)

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	maxlogic "github.com/q191201771/lalmax/logic"

	config "github.com/q191201771/lalmax/config"

	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazahttp"
	"github.com/q191201771/naza/pkg/nazalog"
)

// TODO(chef): refactor 配置参数供外部传入
// TODO(chef): refactor maxTaskLen修改为能表示是阻塞任务的意思
var (
	maxTaskLen                  = 1024
	notifyTimeoutSec            = 3
	hookHistorySize             = 256
	hookSubBufSize              = 64
	hookHTTPPostWorkerIdleAfter = time.Minute
)

var Log = nazalog.GetGlobalLogger()

type hookHTTPPostTask struct {
	url       string
	orderKey  string
	eventName string
	payload   []byte
}

type hookHTTPPostWorker struct {
	queue chan hookHTTPPostTask
}

type HookGroupInfo struct {
	base.EventCommonInfo
	AppName    string `json:"app_name"`
	StreamName string `json:"stream_name"`
}

type HookEvent struct {
	ID        int64           `json:"id"`
	Event     string          `json:"event"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`

	sessionID  string
	streamName string
	appName    string
	groupKeys  []maxlogic.StreamKey
}

const (
	HookEventServerStart      = "on_server_start"
	HookEventUpdate           = "on_update"
	HookEventGroupStart       = "on_group_start"
	HookEventGroupStop        = "on_group_stop"
	HookEventStreamActive     = "on_stream_active"
	HookEventPubStart         = "on_pub_start"
	HookEventPubStop          = "on_pub_stop"
	HookEventSubStart         = "on_sub_start"
	HookEventSubStop          = "on_sub_stop"
	HookEventRelayPullStart   = "on_relay_pull_start"
	HookEventRelayPullStop    = "on_relay_pull_stop"
	HookEventRtmpConnect      = "on_rtmp_connect"
	HookEventHlsMakeTs        = "on_hls_make_ts"
	HookEventStreamChanged    = "on_stream_changed"
	HookEventServerKeepalive  = "on_server_keepalive"
	HookEventStreamNoneReader = "on_stream_none_reader"
	HookEventRtpServerTimeout = "on_rtp_server_timeout"
	HookEventRecordMp4        = "on_record_mp4"
	HookEventPublish          = "on_publish"
	HookEventPlay             = "on_play"
	HookEventStreamNotFound   = "on_stream_not_found"
)

// SubCountFn 查询指定流当前的 sub 数量
// 为什么用回调：避免 HttpNotify 直接依赖 lalsvr，保持解耦
type SubCountFn func(streamName string) int

type HttpNotify struct {
	cfg config.HttpNotifyConfig

	serverId string
	stats    *maxlogic.StatAggregator

	client *http.Client

	subCountFn SubCountFn

	eventID     atomic.Int64
	subID       atomic.Int64
	historyMux  sync.RWMutex
	history     []HookEvent
	subscriberM sync.RWMutex
	subscribers map[int64]chan HookEvent
	pluginMux   sync.RWMutex
	plugins     map[string]*hookPluginEntry
	httpPostMux sync.Mutex
	httpPosts   map[string]*hookHTTPPostWorker
}

// SetSubCountFn 注入 sub 数量查询函数，用于 on_stream_none_reader 判断
func (h *HttpNotify) SetSubCountFn(fn SubCountFn) {
	h.subCountFn = fn
}

// UpdateZlmHookConfig 运行时更新 ZLM 兼容 hook 配置
// 为什么：gb28181 通过 setServerConfig 动态设置 hook URL，需要立即生效
// 为什么清零原有字段：ZLM 回调与 lalmax 原有回调互斥，避免双重触发
func (h *HttpNotify) UpdateZlmHookConfig(zlmCfg config.ZlmCompatHookConfig) {
	h.cfg.ZlmCompatHookConfig = zlmCfg
	h.cfg.Enable = true

	if h.cfg.HookTimeoutSec > 0 {
		h.client.Timeout = time.Duration(h.cfg.HookTimeoutSec) * time.Second
	}

	h.cfg.OnServerStart = ""
	h.cfg.OnUpdate = ""
	h.cfg.OnGroupStart = ""
	h.cfg.OnGroupStop = ""
	h.cfg.OnStreamActive = ""
	h.cfg.OnPubStart = ""
	h.cfg.OnPubStop = ""
	h.cfg.OnSubStart = ""
	h.cfg.OnSubStop = ""
	h.cfg.OnRelayPullStart = ""
	h.cfg.OnRelayPullStop = ""
	h.cfg.OnRtmpConnect = ""
	h.cfg.OnHlsMakeTs = ""

	Log.Infof("zlm compat hook config updated. timeout=%ds, on_stream_changed=%s, on_server_keepalive=%s, on_publish=%s, on_play=%s",
		h.cfg.HookTimeoutSec, zlmCfg.ZlmOnStreamChanged, zlmCfg.ZlmOnServerKeepalive, zlmCfg.ZlmOnPublish, zlmCfg.ZlmOnPlay)
}

func NewHttpNotify(cfg config.HttpNotifyConfig, serverId string) *HttpNotify {
	timeout := notifyTimeoutSec
	if cfg.HookTimeoutSec > 0 {
		timeout = cfg.HookTimeoutSec
	}
	httpNotify := &HttpNotify{
		cfg:         cfg,
		serverId:    serverId,
		stats:       maxlogic.NewStatAggregator(maxlogic.GetGroupManagerInstance()),
		history:     make([]HookEvent, 0, hookHistorySize),
		subscribers: make(map[int64]chan HookEvent),
		plugins:     make(map[string]*hookPluginEntry),
		httpPosts:   make(map[string]*hookHTTPPostWorker),
		client: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
	httpNotify.mustRegisterBuiltinHTTPPlugin()

	return httpNotify
}

// TODO(chef): Dispose

// ---------------------------------------------------------------------------------------------------------------------

func (h *HttpNotify) NotifyServerStart(info base.LalInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventServerStart, info)
}

func (h *HttpNotify) NotifyUpdate(info base.UpdateInfo) {
	info.ServerId = h.serverId
	info.Groups = h.stats.MergeGroups(info.Groups)
	h.publish(HookEventUpdate, info)
}

func (h *HttpNotify) NotifyGroupStart(info HookGroupInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventGroupStart, info)
}

func (h *HttpNotify) NotifyGroupStop(info HookGroupInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventGroupStop, info)
}

func (h *HttpNotify) NotifyStreamActive(info HookGroupInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventStreamActive, info)
}

func (h *HttpNotify) NotifyPubStart(info base.PubStartInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventPubStart, info)

	if !h.cfg.HasZlmHooks() {
		return
	}
	// --- ZLM 兼容：派生 on_publish + on_stream_changed ---
	h.publish(HookEventPublish, ZlmOnPublishPayload{
		MediaServerID: h.serverId,
		App:           info.AppName,
		Schema:        info.Protocol,
		Stream:        info.StreamName,
		Vhost:         "__defaultVhost__",
	})
	h.publish(HookEventStreamChanged, ZlmOnStreamChangedPayload{
		Regist:        true,
		App:           info.AppName,
		Stream:        info.StreamName,
		AppName:       info.AppName,
		StreamName:    info.StreamName,
		Schema:        info.Protocol,
		MediaServerID: h.serverId,
		Vhost:         "__defaultVhost__",
	})
}

func (h *HttpNotify) NotifyPubStop(info base.PubStopInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventPubStop, info)

	if !h.cfg.HasZlmHooks() {
		return
	}
	// --- ZLM 兼容：派生 on_stream_changed(regist=false) ---
	h.publish(HookEventStreamChanged, ZlmOnStreamChangedPayload{
		Regist:        false,
		App:           info.AppName,
		Stream:        info.StreamName,
		AppName:       info.AppName,
		StreamName:    info.StreamName,
		Schema:        info.Protocol,
		MediaServerID: h.serverId,
		Vhost:         "__defaultVhost__",
	})
}

func (h *HttpNotify) NotifySubStart(info base.SubStartInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventSubStart, info)

	if !h.cfg.HasZlmHooks() {
		return
	}
	// --- ZLM 兼容：派生 on_play ---
	h.publish(HookEventPlay, ZlmOnPlayPayload{
		MediaServerID: h.serverId,
		App:           info.AppName,
		Schema:        info.Protocol,
		Stream:        info.StreamName,
		Vhost:         "__defaultVhost__",
	})
}

func (h *HttpNotify) NotifySubStop(info base.SubStopInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventSubStop, info)

	if h.cfg.ZlmOnStreamNoneReader == "" || h.subCountFn == nil {
		return
	}
	// 检查该流是否已无观看者，触发 on_stream_none_reader
	if h.subCountFn(info.StreamName) <= 0 {
		h.NotifyStreamNoneReader(ZlmOnStreamNoneReaderPayload{
			App:    info.AppName,
			Schema: info.Protocol,
			Stream: info.StreamName,
			Vhost:  "__defaultVhost__",
		})
	}
}

func (h *HttpNotify) NotifyPullStart(info base.PullStartInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventRelayPullStart, info)
}

func (h *HttpNotify) NotifyPullStop(info base.PullStopInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventRelayPullStop, info)
}

func (h *HttpNotify) NotifyRtmpConnect(info base.RtmpConnectInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventRtmpConnect, info)
}

func (h *HttpNotify) NotifyOnHlsMakeTs(info base.HlsMakeTsInfo) {
	info.ServerId = h.serverId
	h.publish(HookEventHlsMakeTs, info)
}

func (h *HttpNotify) NotifyStreamChanged(info ZlmOnStreamChangedPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventStreamChanged, info)
}

func (h *HttpNotify) NotifyServerKeepalive() {
	h.publish(HookEventServerKeepalive, ZlmOnServerKeepalivePayload{
		MediaServerID: h.serverId,
	})
}

func (h *HttpNotify) NotifyStreamNoneReader(info ZlmOnStreamNoneReaderPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventStreamNoneReader, info)
}

func (h *HttpNotify) NotifyRtpServerTimeout(info ZlmOnRtpServerTimeoutPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventRtpServerTimeout, info)
}

func (h *HttpNotify) NotifyRecordMp4(info ZlmOnRecordMp4Payload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventRecordMp4, info)
}

func (h *HttpNotify) NotifyPublish(info ZlmOnPublishPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventPublish, info)
}

func (h *HttpNotify) NotifyPlay(info ZlmOnPlayPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventPlay, info)
}

func (h *HttpNotify) NotifyStreamNotFound(info ZlmOnStreamNotFoundPayload) {
	if info.MediaServerID == "" {
		info.MediaServerID = h.serverId
	}
	h.publish(HookEventStreamNotFound, info)
}

// ----- implement INotifyHandler interface ----------------------------------------------------------------------------

func (h *HttpNotify) OnServerStart(info base.LalInfo) {
	h.NotifyServerStart(info)
}

func (h *HttpNotify) OnUpdate(info base.UpdateInfo) {
	h.NotifyUpdate(info)
}

func (h *HttpNotify) OnGroupStart(info HookGroupInfo) {
	h.NotifyGroupStart(info)
}

func (h *HttpNotify) OnGroupStop(info HookGroupInfo) {
	h.NotifyGroupStop(info)
}

func (h *HttpNotify) OnStreamActive(info HookGroupInfo) {
	h.NotifyStreamActive(info)
}

func (h *HttpNotify) OnPubStart(info base.PubStartInfo) {
	h.NotifyPubStart(info)
}

func (h *HttpNotify) OnPubStop(info base.PubStopInfo) {
	h.NotifyPubStop(info)
}

func (h *HttpNotify) OnSubStart(info base.SubStartInfo) {
	h.NotifySubStart(info)
}

func (h *HttpNotify) OnSubStop(info base.SubStopInfo) {
	h.NotifySubStop(info)
}

func (h *HttpNotify) OnRelayPullStart(info base.PullStartInfo) {
	h.NotifyPullStart(info)
}

func (h *HttpNotify) OnRelayPullStop(info base.PullStopInfo) {
	h.NotifyPullStop(info)
}

func (h *HttpNotify) OnRtmpConnect(info base.RtmpConnectInfo) {
	h.NotifyRtmpConnect(info)
}

func (h *HttpNotify) OnHlsMakeTs(info base.HlsMakeTsInfo) {
	h.NotifyOnHlsMakeTs(info)
}

func (h *HttpNotify) asyncPostEvent(url string, event HookEvent) {
	if !h.cfg.Enable || url == "" {
		return
	}

	h.dispatchHTTPPost(h.newHookHTTPPostTask(url, event))
}

func (h *HttpNotify) newHookHTTPPostTask(url string, event HookEvent) hookHTTPPostTask {
	return hookHTTPPostTask{
		url:       url,
		orderKey:  buildHookHTTPOrderKey(url, event),
		eventName: event.Event,
		payload:   append([]byte(nil), event.Payload...),
	}
}

func buildHookHTTPOrderKey(url string, event HookEvent) string {
	if event.Event == HookEventUpdate {
		return url + "|__update__"
	}
	if len(event.groupKeys) == 1 {
		key := event.groupKeys[0]
		if key.AppName != "" && key.StreamName != "" {
			return fmt.Sprintf("__stream__|%s|%s", key.AppName, key.StreamName)
		}
	}
	if event.appName != "" && event.streamName != "" {
		return fmt.Sprintf("__stream__|%s|%s", event.appName, event.streamName)
	}
	return url + "|__global__"
}

func (h *HttpNotify) dispatchHTTPPost(task hookHTTPPostTask) {
	h.httpPostMux.Lock()
	worker, ok := h.httpPosts[task.orderKey]
	if !ok {
		worker = &hookHTTPPostWorker{
			queue: make(chan hookHTTPPostTask, maxTaskLen),
		}
		h.httpPosts[task.orderKey] = worker
		go h.runHTTPPostWorker(task.orderKey, worker)
	}

	select {
	case worker.queue <- task:
	default:
		Log.Warnf("http notify queue full. key=%s, event=%s, url=%s", task.orderKey, task.eventName, task.url)
	}
	h.httpPostMux.Unlock()
}

func (h *HttpNotify) runHTTPPostWorker(orderKey string, worker *hookHTTPPostWorker) {
	timer := time.NewTimer(hookHTTPPostWorkerIdleAfter)
	defer timer.Stop()

	for {
		select {
		case task, ok := <-worker.queue:
			if !ok {
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			h.postRaw(task.url, task.payload)
			timer.Reset(hookHTTPPostWorkerIdleAfter)
		case <-timer.C:
			h.httpPostMux.Lock()
			current, exists := h.httpPosts[orderKey]
			if exists && current == worker && len(worker.queue) == 0 {
				delete(h.httpPosts, orderKey)
				close(worker.queue)
				h.httpPostMux.Unlock()
				return
			}
			h.httpPostMux.Unlock()
			timer.Reset(hookHTTPPostWorkerIdleAfter)
		}
	}
}

func (h *HttpNotify) postRaw(url string, payload []byte) {
	if h == nil || url == "" || len(payload) == 0 {
		return
	}

	body := bytes.NewBuffer(payload)
	client := h.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Post(url, nazahttp.HeaderFieldContentType, body)
	if err != nil {
		Log.Errorf("http notify post raw payload error. err=%+v, url=%s, payload=%s", err, url, string(payload))
		return
	}
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func (h *HttpNotify) Recent(limit int) []HookEvent {
	h.historyMux.RLock()
	defer h.historyMux.RUnlock()

	if limit <= 0 || limit > len(h.history) {
		limit = len(h.history)
	}

	start := len(h.history) - limit
	out := make([]HookEvent, limit)
	copy(out, h.history[start:])
	return out
}

func (h *HttpNotify) RecentFiltered(limit int, filter HookEventFilter) []HookEvent {
	h.historyMux.RLock()
	defer h.historyMux.RUnlock()

	if limit <= 0 || limit > len(h.history) {
		limit = len(h.history)
	}

	out := make([]HookEvent, 0, limit)
	for i := len(h.history) - 1; i >= 0 && len(out) < limit; i-- {
		if !filter.Match(h.history[i]) {
			continue
		}
		out = append(out, h.history[i])
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (h *HttpNotify) Subscribe(buffer int) (int64, <-chan HookEvent, func()) {
	if buffer <= 0 {
		buffer = hookSubBufSize
	}

	id := h.subID.Add(1)
	ch := make(chan HookEvent, buffer)

	h.subscriberM.Lock()
	h.subscribers[id] = ch
	h.subscriberM.Unlock()

	cancel := func() {
		h.subscriberM.Lock()
		if sub, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(sub)
		}
		h.subscriberM.Unlock()
	}

	return id, ch, cancel
}

func (h *HttpNotify) publish(event string, info interface{}) {
	if h == nil {
		return
	}

	payload, err := json.Marshal(info)
	if err != nil {
		Log.Errorf("marshal hook event failed. event=%s, err=%+v", event, err)
		return
	}

	hookEvent := HookEvent{
		ID:        h.eventID.Add(1),
		Event:     event,
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Payload:   payload,
	}
	populateHookEventMeta(&hookEvent, info)

	h.historyMux.Lock()
	h.history = append(h.history, hookEvent)
	if len(h.history) > hookHistorySize {
		h.history = append([]HookEvent(nil), h.history[len(h.history)-hookHistorySize:]...)
	}
	h.historyMux.Unlock()

	h.dispatchPlugins(hookEvent)

	h.subscriberM.RLock()
	stale := make([]int64, 0)
	for id, ch := range h.subscribers {
		select {
		case ch <- hookEvent:
		default:
			stale = append(stale, id)
		}
	}
	h.subscriberM.RUnlock()

	if len(stale) == 0 {
		return
	}

	h.subscriberM.Lock()
	for _, id := range stale {
		if ch, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(ch)
		}
	}
	h.subscriberM.Unlock()
}

func populateHookEventMeta(event *HookEvent, info interface{}) {
	if event == nil || info == nil {
		return
	}

	switch v := info.(type) {
	case base.UpdateInfo:
		event.groupKeys = make([]maxlogic.StreamKey, 0, len(v.Groups))
		for _, group := range v.Groups {
			event.groupKeys = append(event.groupKeys, maxlogic.NewStreamKey(group.AppName, group.StreamName))
		}
	case HookGroupInfo:
		event.streamName = v.StreamName
		event.appName = v.AppName
	case base.PubStartInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.PubStopInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.SubStartInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.SubStopInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.PullStartInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.PullStopInfo:
		populateHookSessionMeta(event, v.SessionEventCommonInfo)
	case base.RtmpConnectInfo:
		event.sessionID = v.SessionId
		event.appName = v.App
	case base.HlsMakeTsInfo:
		event.streamName = v.StreamName
	case ZlmOnStreamChangedPayload:
		event.appName = v.App
		event.streamName = v.Stream
		if event.appName == "" {
			event.appName = v.AppName
		}
		if event.streamName == "" {
			event.streamName = v.StreamName
		}
	case ZlmOnStreamNoneReaderPayload:
		event.appName = v.App
		event.streamName = v.Stream
	case ZlmOnRtpServerTimeoutPayload:
		event.streamName = v.StreamID
	case ZlmOnRecordMp4Payload:
		event.appName = v.App
		event.streamName = v.Stream
	case ZlmOnPublishPayload:
		event.appName = v.App
		event.streamName = v.Stream
	case ZlmOnPlayPayload:
		event.appName = v.App
		event.streamName = v.Stream
	case ZlmOnStreamNotFoundPayload:
		event.appName = v.App
		event.streamName = v.Stream
		if event.appName == "" {
			event.appName = v.AppName
		}
		if event.streamName == "" {
			event.streamName = v.StreamName
		}
	}
}

func populateHookSessionMeta(event *HookEvent, info base.SessionEventCommonInfo) {
	if event == nil {
		return
	}

	event.sessionID = info.SessionId
	event.streamName = info.StreamName
	event.appName = info.AppName
}
