package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lalmax/config"
)

func TestConsoleRoutes(t *testing.T) {
	router := gin.New()
	(&LalMaxServer{}).initConsoleRouter(router)

	tests := []struct {
		path     string
		code     int
		contains string
	}{
		{path: "/console", code: http.StatusOK, contains: "lalmax Console"},
		{path: "/console/streams", code: http.StatusOK, contains: "stream-list"},
		{path: "/console/stream?stream=test", code: http.StatusOK, contains: "preview-video"},
		{path: "/console/runtime", code: http.StatusOK, contains: "runtime-cards"},
		{path: "/console-assets/console.css", code: http.StatusOK, contains: "--accent"},
		{path: "/console-assets/console.js", code: http.StatusOK, contains: "fetchConsolePayload"},
		{path: "/console-assets/mpegts.js", code: http.StatusOK, contains: "mpegts"},
		{path: "/console/unknown", code: http.StatusFound},
		{path: "/console-assets/unknown.js", code: http.StatusNotFound},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tt.code {
			t.Fatalf("%s status = %d, want %d", tt.path, w.Code, tt.code)
		}
		if tt.contains != "" && !strings.Contains(w.Body.String(), tt.contains) {
			t.Fatalf("%s body missing %q", tt.path, tt.contains)
		}
	}
}

func TestBuildConsoleStreamIncludesExtendedSessionsAndURLs(t *testing.T) {
	group := LalmaxStatGroup{
		StreamName:  "camera",
		AudioCodec:  "PCMA",
		VideoCodec:  "H264",
		VideoWidth:  1920,
		VideoHeight: 1080,
		StatPull: base.StatPull{StatSession: base.StatSession{
			SessionId:        "pull-1",
			Protocol:         "RTSP",
			BaseType:         "PULL",
			RemoteAddr:       "192.0.2.10:554",
			ReadBitrateKbits: 1200,
			ReadBytesSum:     4096,
		}},
		StatSubs: []base.StatSub{{StatSession: base.StatSession{
			SessionId:         "sub-1",
			Protocol:          "RTSP",
			BaseType:          "SUB",
			WriteBitrateKbits: 800,
			WroteBytesSum:     2048,
		}}},
		Fps: []base.RecordPerSec{{UnixSec: 1, V: 24}, {UnixSec: 2, V: 26}},
		Lalmax: LalmaxGroupStat{ExtSubs: []base.StatSub{
			{StatSession: base.StatSession{
				SessionId:         "sub-2",
				Protocol:          "RTC",
				BaseType:          "SUB",
				WriteBitrateKbits: 700,
			}},
			{StatSession: base.StatSession{
				SessionId:         "sub-1",
				Protocol:          "RTSP",
				BaseType:          "SUB",
				WriteBitrateKbits: 800,
			}},
		}},
	}
	features := consoleFeatures{HTTPFLV: true, FMP4: true, HLS: true, RTC: true, SRT: true, RTMP: true, RTSP: true, srtPort: 6001, rtmpPort: 1935, rtspPort: 5544}

	stream := buildConsoleStream(group, features, "example.com")
	if stream.Source.SessionID != "pull-1" || stream.Source.Role != "pull" {
		t.Fatalf("source = %+v", stream.Source)
	}
	if stream.Players != 2 || stream.OutputBitrate != 1500 {
		t.Fatalf("players=%d output=%d", stream.Players, stream.OutputBitrate)
	}
	if stream.VideoFPS != 25 {
		t.Fatalf("fps = %f", stream.VideoFPS)
	}
	if stream.Health != "good" {
		t.Fatalf("health = %s", stream.Health)
	}
	for name, value := range map[string]string{
		"flv": stream.URLs.FLV, "fmp4": stream.URLs.FMP4, "hls": stream.URLs.HLS,
		"rtc": stream.URLs.RTC, "srt": stream.URLs.SRT, "rtmp": stream.URLs.RTMP, "rtsp": stream.URLs.RTSP,
	} {
		if value == "" {
			t.Fatalf("%s URL is empty", name)
		}
	}
}

func TestBuildConsoleFeaturesUsesLalConfig(t *testing.T) {
	s := &LalMaxServer{conf: &config.Config{
		SrtConfig: config.SrtConfig{Enable: true, Addr: ":7001"},
		RtcConfig: config.RtcConfig{Enable: true},
		LalRawContent: []byte(`{
			"rtmp":{"enable":true,"addr":":2935"},
			"rtsp":{"enable":true,"addr":":6544"},
			"httpflv":{"enable":true},
			"pprof":{"enable":true,"addr":":9084"}
		}`),
	}}

	features := s.buildConsoleFeatures("monitor.example.com:1290")
	if !features.SRT || !features.RTC || !features.RTMP || !features.RTSP || !features.HTTPFLV || !features.Pprof {
		t.Fatalf("features = %+v", features)
	}
	if features.srtPort != 7001 || features.rtmpPort != 2935 || features.rtspPort != 6544 {
		t.Fatalf("ports = srt:%d rtmp:%d rtsp:%d", features.srtPort, features.rtmpPort, features.rtspPort)
	}
	if features.PprofURL != "http://monitor.example.com:9084/debug/pprof/" {
		t.Fatalf("pprof URL = %s", features.PprofURL)
	}
}
