package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	config "github.com/q191201771/lalmax/config"
)

// initZlmCompatRouter 注册 /index/api/* ZLM 兼容路由
// 为什么独立文件：隔离 ZLM 兼容层，不影响现有 lalmax API
func (s *LalMaxServer) initZlmCompatRouter(router *gin.Engine, handlers ...gin.HandlerFunc) {
	zlm := router.Group("/index/api", handlers...)
	zlm.POST("/openRtpServer", s.zlmOpenRtpServerHandler)
	zlm.POST("/closeRtpServer", s.zlmCloseRtpServerHandler)
	zlm.POST("/close_streams", s.zlmCloseStreamsHandler)
	zlm.POST("/getServerConfig", s.zlmGetServerConfigHandler)
	zlm.POST("/setServerConfig", s.zlmSetServerConfigHandler)
	zlm.POST("/restartServer", s.zlmRestartServerHandler)
	zlm.POST("/startRecord", s.zlmStartRecordHandler)
	zlm.POST("/stopRecord", s.zlmStopRecordHandler)
	zlm.POST("/addStreamProxy", s.zlmAddStreamProxyHandler)
	zlm.POST("/getSnap", s.zlmGetSnapHandler)
	zlm.POST("/webrtc", s.zlmWebrtcHandler)
}

// ---------- openRtpServer ----------

func (s *LalMaxServer) zlmOpenRtpServerHandler(c *gin.Context) {
	var req ZlmOpenRtpServerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmOpenRtpServerResp{Code: -300, Msg: "invalid params"})
		return
	}

	isTcpFlag := 0
	if req.TCPMode > 0 {
		isTcpFlag = 1
	}

	resp := s.rtpPubMgr.Start(base.ApiCtrlStartRtpPubReq{
		StreamName: req.StreamID,
		Port:       req.Port,
		IsTcpFlag:  isTcpFlag,
	})

	if resp.ErrorCode != base.ErrorCodeSucc {
		c.JSON(http.StatusOK, ZlmOpenRtpServerResp{Code: -1, Msg: resp.Desp})
		return
	}

	Log.Infof("zlm compat openRtpServer. stream_id=%s, port=%d", req.StreamID, resp.Data.Port)
	c.JSON(http.StatusOK, ZlmOpenRtpServerResp{Code: 0, Port: resp.Data.Port})
}

// ---------- closeRtpServer ----------

func (s *LalMaxServer) zlmCloseRtpServerHandler(c *gin.Context) {
	var req ZlmCloseRtpServerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmCloseRtpServerResp{Code: -300})
		return
	}

	_, err := s.rtpPubMgr.Stop(req.StreamID, "")
	if err != nil {
		Log.Infof("zlm compat closeRtpServer not found. stream_id=%s", req.StreamID)
		c.JSON(http.StatusOK, ZlmCloseRtpServerResp{Code: 0, Hit: 0})
		return
	}

	Log.Infof("zlm compat closeRtpServer. stream_id=%s", req.StreamID)
	c.JSON(http.StatusOK, ZlmCloseRtpServerResp{Code: 0, Hit: 1})
}

// ---------- close_streams ----------

func (s *LalMaxServer) zlmCloseStreamsHandler(c *gin.Context) {
	var req ZlmCloseStreamsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmCloseStreamsResp{Code: -300})
		return
	}

	streamName := req.Stream
	if streamName == "" {
		c.JSON(http.StatusOK, ZlmCloseStreamsResp{Code: 0, CountHit: 0, CountClosed: 0})
		return
	}

	// 尝试通过 kick_session 关闭所有匹配的 session
	groups := s.lalsvr.StatAllGroup()
	hit := 0
	closed := 0
	for _, g := range groups {
		if g.StreamName != streamName {
			continue
		}
		hit++
		// 关闭 pub session
		if g.StatPub.SessionId != "" {
			resp := s.lalsvr.CtrlKickSession(base.ApiCtrlKickSessionReq{
				StreamName: streamName,
				SessionId:  g.StatPub.SessionId,
			})
			if resp.ErrorCode == base.ErrorCodeSucc {
				closed++
			}
		}
	}

	// 也尝试关闭 RTP pub session
	if _, err := s.rtpPubMgr.Stop(streamName, ""); err == nil {
		if hit == 0 {
			hit++
		}
		closed++
	}

	Log.Infof("zlm compat close_streams. stream=%s, hit=%d, closed=%d", streamName, hit, closed)
	c.JSON(http.StatusOK, ZlmCloseStreamsResp{Code: 0, CountHit: hit, CountClosed: closed})
}

// ---------- getServerConfig ----------

func (s *LalMaxServer) zlmGetServerConfigHandler(c *gin.Context) {
	cfg := buildZlmServerConfig(s.conf)
	c.JSON(http.StatusOK, ZlmGetServerConfigResp{Code: 0, Data: []map[string]any{cfg}})
}

// ---------- setServerConfig ----------

func (s *LalMaxServer) zlmSetServerConfigHandler(c *gin.Context) {
	var params map[string]*string
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusOK, ZlmSetServerConfigResp{
			ZlmFixedHeader: ZlmFixedHeader{Code: -300, Msg: "invalid params"},
		})
		return
	}

	changed := 0
	zlmCfg := s.conf.HttpNotifyConfig.ZlmCompatHookConfig

	hookMap := map[string]*string{
		"hook.on_stream_changed":     &zlmCfg.ZlmOnStreamChanged,
		"hook.on_server_keepalive":   &zlmCfg.ZlmOnServerKeepalive,
		"hook.on_stream_none_reader": &zlmCfg.ZlmOnStreamNoneReader,
		"hook.on_rtp_server_timeout": &zlmCfg.ZlmOnRtpServerTimeout,
		"hook.on_record_mp4":         &zlmCfg.ZlmOnRecordMp4,
		"hook.on_publish":            &zlmCfg.ZlmOnPublish,
		"hook.on_play":               &zlmCfg.ZlmOnPlay,
		"hook.on_stream_not_found":   &zlmCfg.ZlmOnStreamNotFound,
		"hook.on_server_started":     &zlmCfg.ZlmOnServerStarted,
	}

	for key, target := range hookMap {
		if v, ok := params[key]; ok && v != nil && *v != *target {
			*target = *v
			changed++
		}
	}

	// 处理 keepalive 间隔
	if v, ok := params["hook.alive_interval"]; ok && v != nil {
		if interval, err := strconv.Atoi(*v); err == nil && interval > 0 {
			s.conf.HttpNotifyConfig.KeepaliveIntervalSec = interval
			changed++
		}
	}

	// 处理 hook 超时时间
	if v, ok := params["hook.timeoutSec"]; ok && v != nil {
		if timeout, err := strconv.Atoi(*v); err == nil && timeout > 0 {
			s.conf.HttpNotifyConfig.HookTimeoutSec = timeout
			changed++
		}
	}

	// 处理 rtp_proxy.port_range
	if v, ok := params["rtp_proxy.port_range"]; ok && v != nil {
		if portMin, portMax, ok := parsePortRange(*v); ok {
			s.rtpPubMgr.UpdatePortRange(portMin, portMax)
			changed++
		}
	}

	if changed > 0 {
		s.conf.HttpNotifyConfig.Enable = true
		s.notifyHub.UpdateZlmHookConfig(zlmCfg)
		s.conf.HttpNotifyConfig.ZlmCompatHookConfig = zlmCfg

		// 同步清零 conf 中的原有 hook URL
		s.conf.HttpNotifyConfig.OnServerStart = ""
		s.conf.HttpNotifyConfig.OnUpdate = ""
		s.conf.HttpNotifyConfig.OnGroupStart = ""
		s.conf.HttpNotifyConfig.OnGroupStop = ""
		s.conf.HttpNotifyConfig.OnStreamActive = ""
		s.conf.HttpNotifyConfig.OnPubStart = ""
		s.conf.HttpNotifyConfig.OnPubStop = ""
		s.conf.HttpNotifyConfig.OnSubStart = ""
		s.conf.HttpNotifyConfig.OnSubStop = ""
		s.conf.HttpNotifyConfig.OnRelayPullStart = ""
		s.conf.HttpNotifyConfig.OnRelayPullStop = ""
		s.conf.HttpNotifyConfig.OnRtmpConnect = ""
		s.conf.HttpNotifyConfig.OnHlsMakeTs = ""

		if err := s.conf.SaveToFile(); err != nil {
			Log.Errorf("zlm compat setServerConfig persist failed. err=%v", err)
		}
	}

	Log.Infof("zlm compat setServerConfig. changed=%d", changed)
	c.JSON(http.StatusOK, ZlmSetServerConfigResp{
		ZlmFixedHeader: ZlmFixedHeader{Code: 0},
		Changed:        changed,
	})
}

// ---------- restartServer ----------

func (s *LalMaxServer) zlmRestartServerHandler(c *gin.Context) {
	// 为什么不重启：lalmax 不需要像 ZLM 那样通过重启来重绑端口
	Log.Infof("zlm compat restartServer (noop)")
	c.JSON(http.StatusOK, ZlmFixedHeader{Code: 0, Msg: "ok"})
}

// ---------- addStreamProxy ----------

func (s *LalMaxServer) zlmAddStreamProxyHandler(c *gin.Context) {
	var req ZlmAddStreamProxyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmAddStreamProxyResp{ZlmFixedHeader: ZlmFixedHeader{Code: -300, Msg: "invalid params"}})
		return
	}

	streamName := req.Stream
	if streamName == "" {
		c.JSON(http.StatusOK, ZlmAddStreamProxyResp{ZlmFixedHeader: ZlmFixedHeader{Code: -300, Msg: "stream is required"}})
		return
	}

	pullReq := base.ApiCtrlStartRelayPullReq{
		Url:                      req.URL,
		StreamName:               streamName,
		PullTimeoutMs:            int(req.TimeoutSec * 1000),
		PullRetryNum:             req.RetryCount,
		AutoStopPullAfterNoOutMs: base.AutoStopPullAfterNoOutMsNever,
		RtspMode:                 req.RTPType,
	}
	if pullReq.PullRetryNum == 0 {
		pullReq.PullRetryNum = base.PullRetryNumNever
	}
	if pullReq.PullTimeoutMs == 0 {
		pullReq.PullTimeoutMs = logic.DefaultApiCtrlStartRelayPullReqPullTimeoutMs
	}

	resp := s.lalsvr.CtrlStartRelayPull(pullReq)
	if resp.ErrorCode != base.ErrorCodeSucc {
		c.JSON(http.StatusOK, ZlmAddStreamProxyResp{ZlmFixedHeader: ZlmFixedHeader{Code: -1, Msg: resp.Desp}})
		return
	}

	Log.Infof("zlm compat addStreamProxy. stream=%s, session_id=%s", streamName, resp.Data.SessionId)
	var out ZlmAddStreamProxyResp
	out.Code = 0
	out.Data.Key = resp.Data.SessionId
	c.JSON(http.StatusOK, out)
}

// ---------- startRecord ----------

func (s *LalMaxServer) zlmStartRecordHandler(c *gin.Context) {
	var req ZlmStartRecordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmStartRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: -300, Msg: "invalid params"}})
		return
	}

	rtmpAddr := extractHostPort(s.conf, "rtmp")
	if rtmpAddr == "" {
		c.JSON(http.StatusOK, ZlmStartRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: -1, Msg: "rtmp not configured"}})
		return
	}

	_, err := s.recorder.startRecord(rtmpAddr, req.App, req.Stream, req.Type, req.MaxSecond)
	if err != nil {
		Log.Errorf("zlm compat startRecord failed. stream=%s, err=%v", req.Stream, err)
		c.JSON(http.StatusOK, ZlmStartRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: -1, Msg: err.Error()}, Result: false})
		return
	}

	Log.Infof("zlm compat startRecord. app=%s, stream=%s, type=%d", req.App, req.Stream, req.Type)
	c.JSON(http.StatusOK, ZlmStartRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: 0}, Result: true})
}

// ---------- stopRecord ----------

func (s *LalMaxServer) zlmStopRecordHandler(c *gin.Context) {
	var req ZlmStopRecordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmStopRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: -300, Msg: "invalid params"}})
		return
	}

	file, err := s.recorder.stopRecord(req.App, req.Stream, req.Type)
	if err != nil {
		Log.Infof("zlm compat stopRecord not recording. app=%s, stream=%s, err=%v", req.App, req.Stream, err)
		c.JSON(http.StatusOK, ZlmStopRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: 0}, Result: false})
		return
	}

	Log.Infof("zlm compat stopRecord. app=%s, stream=%s, file=%s", req.App, req.Stream, file)
	c.JSON(http.StatusOK, ZlmStopRecordResp{ZlmFixedHeader: ZlmFixedHeader{Code: 0}, Result: true})
}

// ---------- getSnap ----------

func (s *LalMaxServer) zlmGetSnapHandler(c *gin.Context) {
	var req ZlmGetSnapReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, ZlmFixedHeader{Code: -300, Msg: "invalid params"})
		return
	}

	if req.URL == "" {
		c.JSON(http.StatusOK, ZlmFixedHeader{Code: -300, Msg: "url is required"})
		return
	}

	data, err := getSnap(req.URL, req.TimeoutSec)
	if err != nil {
		Log.Errorf("zlm compat getSnap failed. url=%s, err=%v", req.URL, err)
		c.JSON(http.StatusOK, ZlmFixedHeader{Code: -1, Msg: err.Error()})
		return
	}

	Log.Infof("zlm compat getSnap. url=%s, size=%d", req.URL, len(data))
	c.Data(http.StatusOK, "image/jpeg", data)
}

// ---------- webrtc ----------

// zlmWebrtcHandler ZLM 兼容 WebRTC 信令接口
// 为什么：gb28181 前端通过 /index/api/webrtc?app=xx&stream=xx&type=play 播放
func (s *LalMaxServer) zlmWebrtcHandler(c *gin.Context) {
	typ := c.Query("type")
	app := c.Query("app")
	stream := c.Query("stream")

	if stream == "" || typ != "play" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "only type=play supported"})
		return
	}

	if s.rtcsvr == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "webrtc not enabled"})
		return
	}

	body, err := c.GetRawData()
	if err != nil || len(body) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "invalid sdp offer"})
		return
	}

	Log.Infof("zlm compat webrtc play. app=%s, stream=%s", app, stream)

	sdp, err := s.rtcsvr.HandleZlmWebrtcPlay(app, stream, string(body))
	if err != nil {
		Log.Errorf("zlm compat webrtc play failed. app=%s, stream=%s, err=%v", app, stream, err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"id":   s.conf.ServerId,
		"sdp":  sdp,
		"type": "answer",
	})
}

// extractHostPort 从 lal 原始配置中提取指定协议的 host:port
// 为什么有默认值：ZLM 模式下 gb28181 假设 RTMP 总在标准端口可用
func extractHostPort(conf *config.Config, protocol string) string {
	var raw lalRawPorts
	if len(conf.LalRawContent) > 0 {
		_ = json.Unmarshal(conf.LalRawContent, &raw)
	}
	switch protocol {
	case "rtmp":
		addr := raw.Rtmp.Addr
		if addr == "" {
			return "127.0.0.1:1935"
		}
		if addr[0] == ':' {
			return "127.0.0.1" + addr
		}
		return addr
	}
	return ""
}
