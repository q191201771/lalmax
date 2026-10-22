package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
)

//go:embed console.html console.css console.js mpegts.js
var consoleFiles embed.FS

var consoleStartedAt = time.Now()

type consoleFeatures struct {
	HTTPFLV  bool   `json:"http_flv"`
	FMP4     bool   `json:"fmp4"`
	HLS      bool   `json:"hls"`
	RTC      bool   `json:"rtc"`
	SRT      bool   `json:"srt"`
	RTMP     bool   `json:"rtmp"`
	RTSP     bool   `json:"rtsp"`
	Pprof    bool   `json:"pprof"`
	PprofURL string `json:"pprof_url,omitempty"`

	rtmpPort int
	rtspPort int
	srtPort  int
}

type consoleRuntime struct {
	GoVersion      string `json:"go_version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	CPUCount       int    `json:"cpu_count"`
	Goroutines     int    `json:"goroutines"`
	UptimeSec      int64  `json:"uptime_sec"`
	AllocBytes     uint64 `json:"alloc_bytes"`
	HeapInuseBytes uint64 `json:"heap_inuse_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
	GCCount        uint32 `json:"gc_count"`
}

type consoleSession struct {
	Role              string `json:"role"`
	SessionID         string `json:"session_id"`
	Protocol          string `json:"protocol"`
	BaseType          string `json:"base_type"`
	RemoteAddr        string `json:"remote_addr"`
	StartTime         string `json:"start_time"`
	ReadBytesSum      uint64 `json:"read_bytes_sum"`
	WroteBytesSum     uint64 `json:"wrote_bytes_sum"`
	BitrateKbits      int    `json:"bitrate_kbits"`
	ReadBitrateKbits  int    `json:"read_bitrate_kbits"`
	WriteBitrateKbits int    `json:"write_bitrate_kbits"`
}

type consoleStreamURLs struct {
	FLV  string `json:"flv,omitempty"`
	FMP4 string `json:"fmp4,omitempty"`
	HLS  string `json:"hls,omitempty"`
	RTC  string `json:"rtc,omitempty"`
	SRT  string `json:"srt,omitempty"`
	RTMP string `json:"rtmp,omitempty"`
	RTSP string `json:"rtsp,omitempty"`
}

type consoleStream struct {
	StreamName    string              `json:"stream_name"`
	AppName       string              `json:"app_name"`
	DisplayApp    string              `json:"display_app"`
	AudioCodec    string              `json:"audio_codec"`
	VideoCodec    string              `json:"video_codec"`
	VideoWidth    int                 `json:"video_width"`
	VideoHeight   int                 `json:"video_height"`
	VideoFPS      float64             `json:"video_fps"`
	Players       int                 `json:"players"`
	InputBitrate  int                 `json:"input_bitrate_kbits"`
	OutputBitrate int                 `json:"output_bitrate_kbits"`
	ReadBytesSum  uint64              `json:"read_bytes_sum"`
	WroteBytesSum uint64              `json:"wrote_bytes_sum"`
	Health        string              `json:"health"`
	Source        consoleSession      `json:"source"`
	Consumers     []consoleSession    `json:"consumers"`
	FPS           []base.RecordPerSec `json:"fps"`
	URLs          consoleStreamURLs   `json:"urls"`
}

type consoleTotals struct {
	Streams       int `json:"streams"`
	Players       int `json:"players"`
	InputBitrate  int `json:"input_bitrate_kbits"`
	OutputBitrate int `json:"output_bitrate_kbits"`
}

type consolePayload struct {
	Server    base.LalInfo    `json:"server"`
	Runtime   consoleRuntime  `json:"runtime"`
	Features  consoleFeatures `json:"features"`
	Totals    consoleTotals   `json:"totals"`
	Streams   []consoleStream `json:"streams"`
	UpdatedAt string          `json:"updated_at"`
}

type consoleOverviewResp struct {
	base.ApiRespBasic
	Data consolePayload `json:"data"`
}

func (s *LalMaxServer) initConsoleRouter(router *gin.Engine, handlers ...gin.HandlerFunc) {
	router.GET("/console", s.consolePageHandler)
	router.GET("/console/*page", s.consolePageHandler)
	router.GET("/console-assets/:asset", s.consoleAssetHandler)

	api := router.Group("/api/console", handlers...)
	api.GET("/overview", s.consoleOverviewHandler)
}

func (s *LalMaxServer) consolePageHandler(c *gin.Context) {
	switch c.Param("page") {
	case "", "/", "/streams", "/stream", "/runtime":
	default:
		target := "/console/streams"
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusFound, target)
		return
	}

	data, err := consoleFiles.ReadFile("console.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "console unavailable")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

func (s *LalMaxServer) consoleAssetHandler(c *gin.Context) {
	asset := c.Param("asset")
	contentType := ""
	switch asset {
	case "console.css":
		contentType = "text/css; charset=utf-8"
	case "console.js", "mpegts.js":
		contentType = "application/javascript; charset=utf-8"
	default:
		c.Status(http.StatusNotFound)
		return
	}

	data, err := consoleFiles.ReadFile(asset)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, contentType, data)
}

func (s *LalMaxServer) consoleOverviewHandler(c *gin.Context) {
	if s == nil || s.lalsvr == nil || s.stats == nil {
		c.JSON(http.StatusServiceUnavailable, consoleOverviewResp{
			ApiRespBasic: base.ApiRespBasic{ErrorCode: http.StatusServiceUnavailable, Desp: "server unavailable"},
		})
		return
	}

	features := s.buildConsoleFeatures(c.Request.Host)
	views := s.stats.BuildGroupsView(s.lalsvr.StatAllGroup())
	streams := make([]consoleStream, 0, len(views))
	totals := consoleTotals{}
	for _, view := range views {
		stream := buildConsoleStream(newLalmaxStatGroup(view), features, requestConsoleHost(c.Request.Host))
		streams = append(streams, stream)
		totals.Players += stream.Players
		totals.InputBitrate += stream.InputBitrate
		totals.OutputBitrate += stream.OutputBitrate
	}
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Health != streams[j].Health {
			return consoleHealthRank(streams[i].Health) > consoleHealthRank(streams[j].Health)
		}
		if streams[i].StreamName == streams[j].StreamName {
			return streams[i].AppName < streams[j].AppName
		}
		return streams[i].StreamName < streams[j].StreamName
	})
	totals.Streams = len(streams)

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out := consoleOverviewResp{
		ApiRespBasic: base.ApiRespBasic{ErrorCode: base.ErrorCodeSucc, Desp: base.DespSucc},
		Data: consolePayload{
			Server:   s.lalsvr.StatLalInfo(),
			Features: features,
			Totals:   totals,
			Streams:  streams,
			Runtime: consoleRuntime{
				GoVersion:      runtime.Version(),
				OS:             runtime.GOOS,
				Arch:           runtime.GOARCH,
				CPUCount:       runtime.NumCPU(),
				Goroutines:     runtime.NumGoroutine(),
				UptimeSec:      int64(time.Since(consoleStartedAt).Seconds()),
				AllocBytes:     mem.Alloc,
				HeapInuseBytes: mem.HeapInuse,
				SysBytes:       mem.Sys,
				GCCount:        mem.NumGC,
			},
			UpdatedAt: time.Now().Format(time.RFC3339),
		},
	}
	c.JSON(http.StatusOK, out)
}

func buildConsoleStream(group LalmaxStatGroup, features consoleFeatures, host string) consoleStream {
	source := makeConsoleSession("pub", group.StatPub.StatSession)
	if !consoleSessionPresent(source) {
		source = makeConsoleSession("pull", group.StatPull.StatSession)
	}
	if !consoleSessionPresent(source) {
		source = makeConsoleSession("pub", group.Lalmax.ExtPub.StatSession)
	}

	consumers := make([]consoleSession, 0, len(group.StatSubs)+len(group.Lalmax.ExtSubs))
	consumerKeys := make(map[string]struct{}, len(group.StatSubs)+len(group.Lalmax.ExtSubs))
	appendConsumer := func(session base.StatSession) {
		consumer := makeConsoleSession("sub", session)
		key := consumer.SessionID
		if key == "" {
			key = consumer.Protocol + "\x00" + consumer.BaseType + "\x00" + consumer.RemoteAddr
		}
		if _, exists := consumerKeys[key]; exists {
			return
		}
		consumerKeys[key] = struct{}{}
		consumers = append(consumers, consumer)
	}
	for _, sub := range group.StatSubs {
		appendConsumer(sub.StatSession)
	}
	for _, sub := range group.Lalmax.ExtSubs {
		appendConsumer(sub.StatSession)
	}

	inputBitrate := source.ReadBitrateKbits
	if inputBitrate == 0 {
		inputBitrate = source.BitrateKbits
	}
	outputBitrate := 0
	readBytes := source.ReadBytesSum
	wroteBytes := source.WroteBytesSum
	for _, consumer := range consumers {
		bitrate := consumer.WriteBitrateKbits
		if bitrate == 0 {
			bitrate = consumer.BitrateKbits
		}
		outputBitrate += bitrate
		readBytes += consumer.ReadBytesSum
		wroteBytes += consumer.WroteBytesSum
	}

	videoFPS := 0.0
	for _, sample := range group.Fps {
		videoFPS += float64(sample.V)
	}
	if len(group.Fps) > 0 {
		videoFPS /= float64(len(group.Fps))
	}

	health := "good"
	if !consoleSessionPresent(source) {
		health = "bad"
	} else if inputBitrate <= 0 || (group.VideoCodec != "" && len(group.Fps) > 0 && videoFPS == 0) {
		health = "warning"
	}

	displayApp := group.AppName
	if displayApp == "" {
		displayApp = "live"
	}

	return consoleStream{
		StreamName:    group.StreamName,
		AppName:       group.AppName,
		DisplayApp:    displayApp,
		AudioCodec:    group.AudioCodec,
		VideoCodec:    group.VideoCodec,
		VideoWidth:    group.VideoWidth,
		VideoHeight:   group.VideoHeight,
		VideoFPS:      videoFPS,
		Players:       len(consumers),
		InputBitrate:  inputBitrate,
		OutputBitrate: outputBitrate,
		ReadBytesSum:  readBytes,
		WroteBytesSum: wroteBytes,
		Health:        health,
		Source:        source,
		Consumers:     consumers,
		FPS:           group.Fps,
		URLs:          buildConsoleStreamURLs(group.AppName, displayApp, group.StreamName, features, host),
	}
}

func makeConsoleSession(role string, session base.StatSession) consoleSession {
	return consoleSession{
		Role:              role,
		SessionID:         session.SessionId,
		Protocol:          session.Protocol,
		BaseType:          session.BaseType,
		RemoteAddr:        session.RemoteAddr,
		StartTime:         session.StartTime,
		ReadBytesSum:      session.ReadBytesSum,
		WroteBytesSum:     session.WroteBytesSum,
		BitrateKbits:      session.BitrateKbits,
		ReadBitrateKbits:  session.ReadBitrateKbits,
		WriteBitrateKbits: session.WriteBitrateKbits,
	}
}

func consoleSessionPresent(session consoleSession) bool {
	return session.SessionID != "" || session.Protocol != "" || session.BaseType != ""
}

func buildConsoleStreamURLs(appName, displayApp, streamName string, features consoleFeatures, host string) consoleStreamURLs {
	query := url.Values{}
	if appName != "" {
		query.Set("app_name", appName)
	}
	withApp := func(path string) string {
		if len(query) == 0 {
			return path
		}
		return path + "?" + query.Encode()
	}

	urls := consoleStreamURLs{}
	if features.HTTPFLV {
		urls.FLV = "/" + url.PathEscape(displayApp) + "/" + url.PathEscape(streamName) + ".flv"
	}
	if features.FMP4 {
		urls.FMP4 = withApp("/live/m4s/" + url.PathEscape(streamName) + ".mp4")
	}
	if features.HLS {
		urls.HLS = withApp("/live/hls/" + url.PathEscape(streamName) + "/index.m3u8")
	}
	if features.RTC {
		rtcQuery := url.Values{"streamid": {streamName}}
		if appName != "" {
			rtcQuery.Set("app_name", appName)
		}
		urls.RTC = "/webrtc/whep?" + rtcQuery.Encode()
	}
	if features.SRT {
		streamID := "#!::h=" + streamName + ",m=request"
		urls.SRT = fmt.Sprintf("srt://%s?streamid=%s", net.JoinHostPort(host, strconv.Itoa(features.srtPort)), url.QueryEscape(streamID))
	}
	if features.RTMP {
		urls.RTMP = fmt.Sprintf("rtmp://%s/%s/%s", net.JoinHostPort(host, strconv.Itoa(features.rtmpPort)), url.PathEscape(displayApp), url.PathEscape(streamName))
	}
	if features.RTSP {
		urls.RTSP = fmt.Sprintf("rtsp://%s/%s/%s", net.JoinHostPort(host, strconv.Itoa(features.rtspPort)), url.PathEscape(displayApp), url.PathEscape(streamName))
	}
	return urls
}

func consoleHealthRank(health string) int {
	switch health {
	case "bad":
		return 2
	case "warning":
		return 1
	default:
		return 0
	}
}

func (s *LalMaxServer) buildConsoleFeatures(requestHost string) consoleFeatures {
	s.confMux.RLock()
	conf := *s.conf
	s.confMux.RUnlock()

	features := consoleFeatures{
		FMP4:     conf.Fmp4Config.Http.Enable,
		HLS:      conf.Fmp4Config.Hls.Enable,
		RTC:      conf.RtcConfig.Enable,
		SRT:      conf.SrtConfig.Enable,
		srtPort:  consolePort(conf.SrtConfig.Addr, 6001),
		rtmpPort: 1935,
		rtspPort: 5544,
	}

	var raw struct {
		RTMP struct {
			Enable bool   `json:"enable"`
			Addr   string `json:"addr"`
		} `json:"rtmp"`
		RTSP struct {
			Enable bool   `json:"enable"`
			Addr   string `json:"addr"`
		} `json:"rtsp"`
		HTTPFLV struct {
			Enable      bool `json:"enable"`
			EnableHTTPS bool `json:"enable_https"`
		} `json:"httpflv"`
		Pprof struct {
			Enable bool   `json:"enable"`
			Addr   string `json:"addr"`
		} `json:"pprof"`
	}
	if json.Unmarshal(conf.LalRawContent, &raw) == nil {
		features.HTTPFLV = raw.HTTPFLV.Enable || raw.HTTPFLV.EnableHTTPS
		features.RTMP = raw.RTMP.Enable
		features.RTSP = raw.RTSP.Enable
		features.Pprof = raw.Pprof.Enable
		features.rtmpPort = consolePort(raw.RTMP.Addr, features.rtmpPort)
		features.rtspPort = consolePort(raw.RTSP.Addr, features.rtspPort)
		if features.Pprof {
			host := requestConsoleHost(requestHost)
			features.PprofURL = fmt.Sprintf("http://%s/debug/pprof/", net.JoinHostPort(host, strconv.Itoa(consolePort(raw.Pprof.Addr, 8084))))
		}
	}
	return features
}

func requestConsoleHost(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil && host != "" {
		return strings.Trim(host, "[]")
	}
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.Trim(hostport, "[]")
	}
	if hostport == "" {
		return "127.0.0.1"
	}
	return hostport
}

func consolePort(addr string, fallback int) int {
	if strings.HasPrefix(addr, ":") {
		if port, err := strconv.Atoi(strings.TrimPrefix(addr, ":")); err == nil {
			return port
		}
	}
	if _, portText, err := net.SplitHostPort(addr); err == nil {
		if port, err := strconv.Atoi(portText); err == nil {
			return port
		}
	}
	return fallback
}
