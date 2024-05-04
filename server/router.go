package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/q191201771/lalmax/hook"

	"github.com/q191201771/lalmax/gb28181"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazahttp"
	"github.com/q191201771/naza/pkg/nazajson"
	"github.com/q191201771/naza/pkg/nazalog"
)

func (s *LalMaxServer) InitRouter(router *gin.Engine) {
	if router == nil {
		return
	}
	router.Use(s.Cors())
	// whip
	router.POST("/whip", s.HandleWHIP)
	router.OPTIONS("/whip", s.HandleWHIP)
	router.DELETE("/whip", s.HandleWHIP)

	// whep
	router.POST("/whep", s.HandleWHEP)
	router.OPTIONS("/whep", s.HandleWHEP)
	router.DELETE("/whep", s.HandleWHEP)
	//Jessibuca flv封装play
	router.POST("/webrtc/play/live/:streamid", s.HandleJessibuca)
	router.DELETE("/webrtc/play/live/:streamid", s.HandleJessibuca)

	// http-fmp4
	router.GET("/live/m4s/:streamid", s.HandleHttpFmp4)

	// hls-fmp4/llhls
	router.GET("/live/hls/:streamid/:type", s.HandleHls)

	// onvif
	router.POST("/api/ctrl/onvif/pull", s.HandleOnvifPull)

	// gb
	gbLogic := gb28181.NewGbLogic(s.gbsbr)
	router.GET("/api/gb/device_infos", gbLogic.GetDeviceInfos)
	router.POST("/api/gb/start_play", gbLogic.StartPlay)
	router.POST("/api/gb/stop_play", gbLogic.StopPlay)
	router.POST("/api/gb/update_all_notify", gbLogic.UpdateAllNotify)
	router.POST("/api/gb/update_notify", gbLogic.UpdateNotify)

	auth := Authentication(s.conf.HttpConfig.CtrlAuthWhitelist.Secrets, s.conf.HttpConfig.CtrlAuthWhitelist.IPs)
	// stat
	router.GET("/api/stat/group", auth, s.statGroupHandler)
	router.GET("/api/stat/all_group", auth, s.statAllGroupHandler)
	router.GET("/api/stat/lal_info", auth, s.statLalInfoHandler)

	// ctrl
	router.POST("/api/ctrl/start_relay_pull", auth, s.ctrlStartRelayPullHandler)
	router.POST("/api/ctrl/stop_relay_pull", auth, s.ctrlStopRelayPullHandler)
	router.POST("/api/ctrl/kick_session", auth, s.ctrlKickSessionHandler)
	router.POST("/api/ctrl/start_rtp_pub", auth, s.ctrlStartRtpPubHandler)
}

func (s *LalMaxServer) HandleWHIP(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHIP(c)
		}
	case "DELETE":
		// TODO 实现DELETE
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleWHEP(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHEP(c)
		}
	case "DELETE":
		// TODO 实现DELETE
		c.Status(http.StatusOK)
	}
}
func (s *LalMaxServer) HandleJessibuca(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleJessibuca(c)
		}
	case "DELETE":
		// TODO 实现DELETE
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleHls(c *gin.Context) {
	if s.hlssvr != nil {
		s.hlssvr.HandleRequest(c)
	} else {
		nazalog.Error("hls is disable")
		c.Status(http.StatusNotFound)
	}
}

func (s *LalMaxServer) HandleHttpFmp4(c *gin.Context) {
	if s.httpfmp4svr != nil {
		s.httpfmp4svr.HandleRequest(c)
	} else {
		nazalog.Error("http-fmp4 is disable")
		c.Status(http.StatusNotFound)
	}
}

func (s *LalMaxServer) HandleOnvifPull(c *gin.Context) {
	if s.onvifsvr != nil {
		s.onvifsvr.HandlePull(c)
	}
}

func (s *LalMaxServer) statGroupHandler(c *gin.Context) {
	var v base.ApiStatGroupResp
	streamName := c.Query("stream_name")
	if streamName == "" {
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}
	v.Data = s.lalsvr.StatGroup(streamName)
	if v.Data == nil {
		v.ErrorCode = base.ErrorCodeGroupNotFound
		v.Desp = base.DespGroupNotFound
		c.JSON(http.StatusOK, v)
		return
	}
	exist, session := hook.GetHookSessionManagerInstance().GetHookSession(streamName)
	if exist {
		v.Data.StatSubs = append(v.Data.StatSubs, session.GetAllConsumer()...)
	}
	v.ErrorCode = base.ErrorCodeSucc
	v.Desp = base.DespSucc
	c.JSON(http.StatusOK, v)
}

func (s *LalMaxServer) statAllGroupHandler(c *gin.Context) {
	var out base.ApiStatAllGroupResp
	out.ErrorCode = base.ErrorCodeSucc
	out.Desp = base.DespSucc
	groups := s.lalsvr.StatAllGroup()
	for i, group := range groups {
		exist, session := hook.GetHookSessionManagerInstance().GetHookSession(group.StreamName)
		if exist {
			groups[i].StatSubs = append(groups[i].StatSubs, session.GetAllConsumer()...)
		}
	}
	out.Data.Groups = groups
	c.JSON(http.StatusOK, out)
}

func (s *LalMaxServer) statLalInfoHandler(c *gin.Context) {
	var v base.ApiStatLalInfoResp
	v.ErrorCode = base.ErrorCodeSucc
	v.Desp = base.DespSucc
	v.Data = s.lalsvr.StatLalInfo()
	c.JSON(http.StatusOK, v)
}

func (s *LalMaxServer) ctrlStartRelayPullHandler(c *gin.Context) {
	var info base.ApiCtrlStartRelayPullReq
	var v base.ApiCtrlStartRelayPullResp
	j, err := unmarshalRequestJSONBody(c.Request, &info, "url")
	if err != nil {
		Log.Warnf("http api start pull error. err=%+v", err)
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}

	if !j.Exist("pull_timeout_ms") {
		info.PullTimeoutMs = logic.DefaultApiCtrlStartRelayPullReqPullTimeoutMs
	}
	if !j.Exist("pull_retry_num") {
		info.PullRetryNum = base.PullRetryNumNever
	}
	if !j.Exist("auto_stop_pull_after_no_out_ms") {
		info.AutoStopPullAfterNoOutMs = base.AutoStopPullAfterNoOutMsNever
	}
	if !j.Exist("rtsp_mode") {
		info.RtspMode = base.RtspModeTcp
	}

	Log.Infof("http api start pull. req info=%+v", info)

	resp := s.lalsvr.CtrlStartRelayPull(info)
	c.JSON(http.StatusOK, resp)
}

func (s *LalMaxServer) ctrlStopRelayPullHandler(c *gin.Context) {
	var v base.ApiCtrlStopRelayPullResp
	streamName := c.Query("stream_name")
	if streamName == "" {
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}

	Log.Infof("http api stop pull. stream_name=%s", streamName)

	resp := s.lalsvr.CtrlStopRelayPull(streamName)
	c.JSON(http.StatusOK, resp)
}

func (s *LalMaxServer) ctrlKickSessionHandler(c *gin.Context) {
	var v base.ApiCtrlKickSessionResp
	var info base.ApiCtrlKickSessionReq

	_, err := unmarshalRequestJSONBody(c.Request, &info, "stream_name", "session_id")
	if err != nil {
		Log.Warnf("http api kick session error. err=%+v", err)
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}

	Log.Infof("http api kick session. req info=%+v", info)

	resp := s.lalsvr.CtrlKickSession(info)
	c.JSON(http.StatusOK, resp)
}

func (s *LalMaxServer) ctrlStartRtpPubHandler(c *gin.Context) {
	var v base.ApiCtrlStartRtpPubResp
	var info base.ApiCtrlStartRtpPubReq

	j, err := unmarshalRequestJSONBody(c.Request, &info, "stream_name")
	if err != nil {
		Log.Warnf("http api start rtp pub error. err=%+v", err)
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}

	if !j.Exist("timeout_ms") {
		info.TimeoutMs = logic.DefaultApiCtrlStartRtpPubReqTimeoutMs
	}

	Log.Infof("http api start rtp pub. req info=%+v", info)

	lal := s.lalsvr.(*logic.ServerManager)
	resp := lal.CtrlStartRtpPub(info)
	c.JSON(http.StatusOK, resp)
}

func unmarshalRequestJSONBody(r *http.Request, info interface{}, keyFieldList ...string) (nazajson.Json, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nazajson.Json{}, err
	}

	j, err := nazajson.New(body)
	if err != nil {
		return j, err
	}
	for _, kf := range keyFieldList {
		if !j.Exist(kf) {
			return j, nazahttp.ErrParamMissing
		}
	}

	return j, json.Unmarshal(body, info)
}
