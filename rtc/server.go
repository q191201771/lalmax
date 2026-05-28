package rtc

import (
	"fmt"
	"net"
	"net/http"
	"time"

	config "github.com/q191201771/lalmax/config"
	maxlogic "github.com/q191201771/lalmax/logic"

	"github.com/gin-gonic/gin"
	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

// StreamNotFoundFn 流不存在时的回调，触发 on_stream_not_found 通知上层拉流
type StreamNotFoundFn func(app, stream, schema string)

type RtcServer struct {
	config           config.RtcConfig
	lalServer        logic.ILalServer
	udpMux           ice.UDPMux
	tcpMux           ice.TCPMux
	streamNotFoundFn StreamNotFoundFn
}

// SetStreamNotFoundFn 注入流不存在回调
func (s *RtcServer) SetStreamNotFoundFn(fn StreamNotFoundFn) {
	s.streamNotFoundFn = fn
}

// waitStreamReady 触发 on_stream_not_found 后轮询等待流就绪
// 为什么：WebRTC 播放请求先于 GB28181 设备推流到达，需通知上层拉流后等待
func (s *RtcServer) waitStreamReady(appName, streamid, schema string) bool {
	key := maxlogic.NewStreamKey(appName, streamid)
	if ok, _ := maxlogic.GetGroupManagerInstance().GetGroup(key); ok {
		return true
	}

	if s.streamNotFoundFn != nil {
		nazalog.Infof("stream not found, triggering on_stream_not_found. app=%s, stream=%s", appName, streamid)
		s.streamNotFoundFn(appName, streamid, schema)
	}

	ok, _ := maxlogic.GetGroupManagerInstance().WaitGroup(key, 500*time.Millisecond, 5*time.Second)
	return ok
}

func NewRtcServer(config config.RtcConfig, lal logic.ILalServer) (*RtcServer, error) {
	var udpMux ice.UDPMux
	var tcpMux ice.TCPMux

	if config.ICEUDPMuxPort != 0 {
		var udplistener *net.UDPConn

		udplistener, err := net.ListenUDP("udp", &net.UDPAddr{
			IP:   net.IP{0, 0, 0, 0},
			Port: config.ICEUDPMuxPort,
		})

		if err != nil {
			nazalog.Error(err)
			return nil, err
		}
		nazalog.Infof("webrtc ice udp listen. port=%d", config.ICEUDPMuxPort)
		udpMux = webrtc.NewICEUDPMux(nil, udplistener)
	}
	if config.WriteChanSize == 0 {
		config.WriteChanSize = 1024
	}
	if config.ICETCPMuxPort != 0 {
		var tcplistener *net.TCPListener

		tcplistener, err := net.ListenTCP("tcp", &net.TCPAddr{
			IP:   net.IP{0, 0, 0, 0},
			Port: config.ICETCPMuxPort,
		})

		if err != nil {
			nazalog.Error(err)
			return nil, err
		}
		nazalog.Infof("webrtc ice tcp listen. port=%d", config.ICETCPMuxPort)
		tcpMux = webrtc.NewICETCPMux(nil, tcplistener, 20)
	}

	svr := &RtcServer{
		config:    config,
		lalServer: lal,
		udpMux:    udpMux,
		tcpMux:    tcpMux,
	}

	return svr, nil
}

func (s *RtcServer) HandleWHIP(c *gin.Context) {
	streamid := c.Request.URL.Query().Get("streamid")
	if streamid == "" {
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	body, err := c.GetRawData()
	if err != nil {
		nazalog.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		nazalog.Error("invalid body")
		c.Status(http.StatusNoContent)
		return
	}

	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	whipsession := NewWhipSession(streamid, pc, s.lalServer)
	if whipsession == nil {
		c.Status(http.StatusInternalServerError)
		pc.Close()
		return
	}
	whipsession.bindRegistry()

	c.Header("Location", fmt.Sprintf("/webrtc/whip/%s", whipsession.subscriberId))

	sdp := whipsession.GetAnswerSDP(string(body))
	if sdp == "" {
		c.Status(http.StatusInternalServerError)
		whipsession.Close()
		return
	}

	go whipsession.Run()

	c.Data(http.StatusCreated, "application/sdp", []byte(sdp))
}

func (s *RtcServer) HandleWHIPDelete(c *gin.Context) {
	resourceID := c.Param("resourceId")
	if resourceID == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	if maxlogic.KickCustomizePub("", resourceID) {
		c.Status(http.StatusOK)
		return
	}
	c.Status(http.StatusNotFound)
}

// ServeWHIPPublishPage 返回内嵌推流页：浏览器直接打开 WHIP URL 即可通过 WHIP POST 建立 WebRTC 推流（与 ServeWHEPPlayPage 对称）。
func (s *RtcServer) ServeWHIPPublishPage(c *gin.Context) {
	if c.Request.URL.Query().Get("streamid") == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, "<!doctype html><meta charset=utf-8><title>WHIP</title><p>缺少查询参数 <code>streamid</code>。示例：<code>/webrtc/whip?streamid=test110</code></p>")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Accept-Post", "application/sdp")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	c.Header("Access-Control-Expose-Headers", "Location")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(buildWHIPPublishHTML()))
}

func buildWHIPPublishHTML() string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>WHIP Publisher</title><style>body{margin:0;background:#0f172a;color:#e2e8f0;font:14px/1.4 system-ui}main{max-width:960px;margin:0 auto;padding:24px}video{width:100%;max-height:360px;background:#000;border-radius:12px}pre{white-space:pre-wrap;background:#111827;padding:12px;border-radius:12px;min-height:72px}</style></head><body><main><p>本页使用摄像头/麦克风通过 WHIP 推流（H264+Opus）。请允许浏览器媒体权限。</p><video id=\"preview\" autoplay muted playsinline></video><pre id=\"log\">connecting...</pre></main><script>(async()=>{const log=(m)=>{document.getElementById('log').textContent=m};const preview=document.getElementById('preview');try{if(!navigator.mediaDevices||!navigator.mediaDevices.getUserMedia){log('当前环境不支持 getUserMedia');return}const stream=await navigator.mediaDevices.getUserMedia({video:true,audio:true});preview.srcObject=stream;const pc=new RTCPeerConnection();stream.getTracks().forEach(t=>pc.addTrack(t,stream));const offer=await pc.createOffer();await pc.setLocalDescription(offer);await new Promise(r=>{if(pc.iceGatheringState==='complete')return r();pc.addEventListener('icegatheringstatechange',()=>{if(pc.iceGatheringState==='complete')r()})});const res=await fetch(location.href,{method:'POST',headers:{'Content-Type':'application/sdp'},body:pc.localDescription.sdp});if(!res.ok){log('WHIP 失败: '+res.status+' '+await res.text());return}const answer=await res.text();await pc.setRemoteDescription({type:'answer',sdp:answer});log('WHIP 已连接，正在推流: '+location.href)}catch(e){log('错误: '+(e&&e.message?e.message:String(e)))}})();</script></body></html>"
}

func (s *RtcServer) HandleJessibuca(c *gin.Context) {
	streamid := c.Param("streamid")
	if streamid == "" {
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	appName := c.Query("app_name")

	body, err := c.GetRawData()
	if err != nil {
		nazalog.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		nazalog.Error("invalid body")
		c.Status(http.StatusNoContent)
		return
	}

	if !s.waitStreamReady(appName, streamid, "rtsp") {
		nazalog.Errorf("stream not ready after waiting. app=%s, stream=%s", appName, streamid)
		c.Status(http.StatusNotFound)
		return
	}

	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	jessibucaSession := NewJessibucaSession(appName, streamid, s.config.WriteChanSize, pc, s.lalServer)
	if jessibucaSession == nil {
		c.Status(http.StatusInternalServerError)
		pc.Close()
		return
	}

	c.Header("Location", fmt.Sprintf("jessibucaflv/%s", jessibucaSession.subscriberId))

	sdp := jessibucaSession.GetAnswerSDP(string(body))
	if sdp == "" {
		c.Status(http.StatusInternalServerError)
		jessibucaSession.Close()
		return
	}

	go jessibucaSession.Run()

	c.Data(http.StatusCreated, "application/sdp", []byte(sdp))
}

// ServeWHEPPlayPage 返回内嵌播放页（与 topsmedia/pkg/httpflv handleWHEPPage + buildWHEPPage 对齐）。规范地址：GET /webrtc/whep?streamid=...
func (s *RtcServer) ServeWHEPPlayPage(c *gin.Context) {
	if c.Request.URL.Query().Get("streamid") == "" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, "<!doctype html><meta charset=utf-8><title>WHEP</title><p>缺少查询参数 <code>streamid</code>。示例：<code>/webrtc/whep?streamid=test110</code> 或带 <code>app_name</code>：<code>/webrtc/whep?streamid=live/test110&amp;app_name=live</code></p>")
		return
	}
	// 与 httpflv.handleWHEPPage 响应头一致（Gin 由框架管理 Connection，不设 close）
	c.Header("Cache-Control", "no-store")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	c.Header("Access-Control-Expose-Headers", "Location")
	c.Header("Accept-Post", "application/sdp")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(buildWHEPPlayHTML()))
}

// buildWHEPPlayHTML 与 topsmedia/pkg/httpflv buildWHEPPage 内嵌脚本与结构保持一致。
func buildWHEPPlayHTML() string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>WHEP Player</title><style>body{margin:0;background:#0f172a;color:#e2e8f0;font:14px/1.4 system-ui}main{max-width:960px;margin:0 auto;padding:24px}video{width:100%;background:#000;border-radius:12px}pre{white-space:pre-wrap;background:#111827;padding:12px;border-radius:12px;min-height:72px}</style></head><body><main><video id=\"video\" autoplay playsinline controls muted></video><pre id=\"log\">connecting...</pre></main><script>(async()=>{const log=(m)=>document.getElementById('log').textContent=m;const video=document.getElementById('video');const pc=new RTCPeerConnection();pc.ontrack=(e)=>{video.srcObject=e.streams[0];};pc.addTransceiver('video',{direction:'recvonly'});pc.addTransceiver('audio',{direction:'recvonly'});const offer=await pc.createOffer();await pc.setLocalDescription(offer);await new Promise(r=>{if(pc.iceGatheringState==='complete')return r();pc.addEventListener('icegatheringstatechange',()=>pc.iceGatheringState==='complete'&&r(),{once:false});});const res=await fetch(location.href,{method:'POST',headers:{'Content-Type':'application/sdp'},body:pc.localDescription.sdp});if(!res.ok){log('whep failed: '+res.status+' '+await res.text());return;}const answer=await res.text();await pc.setRemoteDescription({type:'answer',sdp:answer});log('connected: '+location.href);})();</script></body></html>"
}

func (s *RtcServer) HandleWHEP(c *gin.Context) {
	streamid := c.Request.URL.Query().Get("streamid")
	if streamid == "" {
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	appName := c.Request.URL.Query().Get("app_name")

	body, err := c.GetRawData()
	if err != nil {
		nazalog.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		nazalog.Error("invalid body")
		c.Status(http.StatusNoContent)
		return
	}

	if !s.waitStreamReady(appName, streamid, "rtsp") {
		nazalog.Errorf("stream not ready after waiting. app=%s, stream=%s", appName, streamid)
		c.Status(http.StatusNotFound)
		return
	}

	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	whepsession := NewWhepSession(appName, streamid, s.config.WriteChanSize, pc, s.lalServer)
	if whepsession == nil {
		c.Status(http.StatusInternalServerError)
		pc.Close()
		return
	}

	c.Header("Location", fmt.Sprintf("whep/%s", whepsession.subscriberId))

	sdp := whepsession.GetAnswerSDP(string(body))
	if sdp == "" {
		c.Status(http.StatusInternalServerError)
		whepsession.Close()
		return
	}

	go whepsession.Run()

	c.Data(http.StatusCreated, "application/sdp", []byte(sdp))
}

// HandleZlmWebrtcPlay ZLM 兼容 WebRTC 播放，返回 SDP answer
// 为什么独立方法：ZLM 信令格式为 JSON {"code":0,"sdp":"..."}，与 WHEP 纯 SDP 不同
func (s *RtcServer) HandleZlmWebrtcPlay(app, stream, offer string) (string, error) {
	if !s.waitStreamReady(app, stream, "rtsp") {
		return "", fmt.Errorf("stream not found: %s/%s", app, stream)
	}

	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		return "", fmt.Errorf("create peer connection: %w", err)
	}

	session := NewWhepSession(app, stream, s.config.WriteChanSize, pc, s.lalServer)
	if session == nil {
		pc.Close()
		return "", fmt.Errorf("create session failed: %s/%s", app, stream)
	}

	sdp := session.GetAnswerSDP(offer)
	if sdp == "" {
		session.Close()
		return "", fmt.Errorf("generate answer sdp failed")
	}

	go session.Run()
	return sdp, nil
}
