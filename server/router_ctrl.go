package server

import (
	"net/http"

	maxlogic "github.com/q191201771/lalmax/logic"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
)

func (s *LalMaxServer) initCtrlRouter(router *gin.Engine, handlers ...gin.HandlerFunc) {
	ctrl := router.Group("/api/ctrl", handlers...)
	ctrl.POST("/start_relay_pull", s.ctrlStartRelayPullHandler)
	ctrl.GET("/stop_relay_pull", s.ctrlStopRelayPullHandler)
	ctrl.POST("/stop_relay_pull", s.ctrlStopRelayPullHandler)
	ctrl.POST("/kick_session", s.ctrlKickSessionHandler)
	ctrl.POST("/start_rtp_pub", s.ctrlStartRtpPubHandler)
	ctrl.POST("/stop_rtp_pub", s.ctrlStopRtpPubHandler)
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

	if maxlogic.KickCustomizePub(info.StreamName, info.SessionId) {
		v.ErrorCode = base.ErrorCodeSucc
		v.Desp = base.DespSucc
		c.JSON(http.StatusOK, v)
		return
	}

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

	resp := s.rtpPubMgr.Start(info)
	c.JSON(http.StatusOK, resp)
}

func (s *LalMaxServer) ctrlStopRtpPubHandler(c *gin.Context) {
	var v base.ApiCtrlStopRelayPullResp
	streamName := c.Query("stream_name")
	sessionID := c.Query("session_id")

	if streamName == "" && sessionID == "" {
		var info base.ApiCtrlKickSessionReq
		if _, err := unmarshalRequestJSONBody(c.Request, &info); err == nil {
			streamName = info.StreamName
			sessionID = info.SessionId
		}
	}

	if streamName == "" && sessionID == "" {
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}

	Log.Infof("http api stop rtp pub. stream_name=%s, session_id=%s", streamName, sessionID)

	session, err := s.rtpPubMgr.Stop(streamName, sessionID)
	if err != nil {
		v.ErrorCode = base.ErrorCodeSessionNotFound
		v.Desp = err.Error()
		c.JSON(http.StatusOK, v)
		return
	}

	v.ErrorCode = base.ErrorCodeSucc
	v.Desp = base.DespSucc
	v.Data.SessionId = session.ID
	c.JSON(http.StatusOK, v)
}
