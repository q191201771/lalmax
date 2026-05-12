package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/q191201771/lalmax/srt"

	"github.com/q191201771/lalmax/rtc"

	"github.com/q191201771/lalmax/gb28181/rtppub"

	maxlogic "github.com/q191201771/lalmax/logic"

	httpfmp4 "github.com/q191201771/lalmax/fmp4/http-fmp4"

	"github.com/q191201771/lalmax/fmp4/hls"

	config "github.com/q191201771/lalmax/config"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type LalMaxServer struct {
	lalsvr      logic.ILalServer
	conf        *config.Config
	stats       *maxlogic.StatAggregator
	notifyHub   *HttpNotify
	srtsvr      *srt.SrtServer
	rtcsvr      *rtc.RtcServer
	router      *gin.Engine
	routerTls   *gin.Engine
	httpfmp4svr *httpfmp4.HttpFmp4Server
	hlssvr      *hls.HlsServer
	rtpPubMgr   *rtppub.Manager
	recorder    *ffmpegRecorder
}

func NewLalMaxServer(conf *config.Config) (*LalMaxServer, error) {
	notifyHub := NewHttpNotify(conf.HttpNotifyConfig, conf.ServerId)
	lalsvr := logic.NewLalServer(func(option *logic.Option) {
		if len(conf.LalRawContent) != 0 {
			option.ConfRawContent = conf.LalRawContent
		} else {
			option.ConfFilename = conf.LalSvrConfigPath
		}
		option.NotifyHandler = notifyHub
	})

	maxsvr := &LalMaxServer{
		lalsvr:    lalsvr,
		conf:      conf,
		stats:     maxlogic.NewStatAggregator(maxlogic.GetGroupManagerInstance()),
		notifyHub: notifyHub,
		rtpPubMgr: rtppub.NewManager(lalsvr, conf.GB28181Config.MediaConfig),
		recorder:  newFfmpegRecorder(""),
	}

	// 注入 sub 数量查询，用于 on_stream_none_reader 判断
	notifyHub.SetSubCountFn(func(streamName string) int {
		for _, g := range lalsvr.StatAllGroup() {
			if g.StreamName == streamName {
				return len(g.StatSubs)
			}
		}
		return 0
	})

	if conf.SrtConfig.Enable {
		maxsvr.srtsvr = srt.NewSrtServer(conf.SrtConfig.Addr, lalsvr, func(option *srt.SrtOption) {
			option.Latency = 300
			option.PeerLatency = 300
		})
	}

	if conf.RtcConfig.Enable {
		var err error
		maxsvr.rtcsvr, err = rtc.NewRtcServer(conf.RtcConfig, lalsvr)
		if err != nil {
			nazalog.Error("create rtc svr failed, err:", err)
			return nil, err
		}
		maxsvr.rtcsvr.SetStreamNotFoundFn(func(app, stream, schema string) {
			notifyHub.NotifyStreamNotFound(ZlmOnStreamNotFoundPayload{
				MediaServerID: conf.ServerId,
				App:           app,
				Stream:        stream,
				Schema:        schema,
				Vhost:         "__defaultVhost__",
			})
		})
	}

	if conf.Fmp4Config.Http.Enable {
		maxsvr.httpfmp4svr = httpfmp4.NewHttpFmp4Server()
	}

	if conf.Fmp4Config.Hls.Enable {
		maxsvr.hlssvr = hls.NewHlsServer(conf.Fmp4Config.Hls)
	}

	maxsvr.router = gin.Default()
	maxsvr.InitRouter(maxsvr.router)
	if conf.HttpConfig.EnableHttps {
		maxsvr.routerTls = gin.Default()
		maxsvr.InitRouter(maxsvr.routerTls)
	}

	return maxsvr, nil
}

func (s *LalMaxServer) Run() (err error) {
	s.lalsvr.WithOnHookSession(func(uniqueKey string, streamName string) logic.ICustomizeHookSessionContext {
		key := maxlogic.StreamKeyFromStreamName(streamName)
		group, created := maxlogic.GetGroupManagerInstance().GetOrCreateGroupByStreamName(uniqueKey, streamName, s.hlssvr, s.conf.LogicConfig.GopCacheNum, s.conf.LogicConfig.SingleGopMaxFrameNum)
		group.BindActiveHook(key, func(activeKey maxlogic.StreamKey) {
			if s.notifyHub == nil || !activeKey.Valid() {
				return
			}
			s.notifyHub.NotifyStreamActive(HookGroupInfo{
				AppName:    activeKey.AppName,
				StreamName: activeKey.StreamName,
			})
		})
		group.BindStopHook(key, func(stopKey maxlogic.StreamKey) {
			if s.notifyHub == nil || !stopKey.Valid() {
				return
			}
			s.notifyHub.NotifyGroupStop(HookGroupInfo{
				AppName:    stopKey.AppName,
				StreamName: stopKey.StreamName,
			})
		})
		if created && s.notifyHub != nil {
			s.notifyHub.NotifyGroupStart(HookGroupInfo{
				AppName:    key.AppName,
				StreamName: key.StreamName,
			})
		}
		return group
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if s.srtsvr != nil {
		go s.srtsvr.Run(ctx)
	}

	go s.runPeriodicUpdate(ctx)
	go s.runPeriodicKeepalive(ctx)

	go func() {
		nazalog.Infof("lalmax http listen. addr=%s", s.conf.HttpConfig.ListenAddr)
		if err = s.router.Run(s.conf.HttpConfig.ListenAddr); err != nil {
			nazalog.Infof("lalmax http stop. addr=%s", s.conf.HttpConfig.ListenAddr)
		}
	}()

	if s.conf.HttpConfig.EnableHttps {
		server := &http.Server{Addr: s.conf.HttpConfig.HttpsListenAddr, Handler: s.routerTls, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
		go func() {
			nazalog.Infof("lalmax https listen. addr=%s", s.conf.HttpConfig.HttpsListenAddr)
			if err = server.ListenAndServeTLS(s.conf.HttpConfig.HttpsCertFile, s.conf.HttpConfig.HttpsKeyFile); err != nil {
				nazalog.Infof("lalmax https stop. addr=%s", s.conf.HttpConfig.ListenAddr)
			}
		}()
	}

	return s.lalsvr.RunLoop()
}

func (s *LalMaxServer) runPeriodicUpdate(ctx context.Context) {
	if s == nil || s.notifyHub == nil || s.lalsvr == nil {
		return
	}

	intervalSec := s.conf.HttpNotifyConfig.UpdateIntervalSec
	if intervalSec <= 0 {
		return
	}

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.notifyHub.NotifyUpdate(base.UpdateInfo{
				Groups: s.lalsvr.StatAllGroup(),
			})
		}
	}
}

// runPeriodicKeepalive ZLM 兼容：定时发送 on_server_keepalive
func (s *LalMaxServer) runPeriodicKeepalive(ctx context.Context) {
	if s == nil || s.notifyHub == nil {
		return
	}

	intervalSec := s.conf.HttpNotifyConfig.KeepaliveIntervalSec
	if intervalSec <= 0 {
		return
	}

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.notifyHub.NotifyServerKeepalive()
		}
	}
}

func (s *LalMaxServer) HookHub() *HttpNotify {
	return s.notifyHub
}

func (s *LalMaxServer) RegisterHookPlugin(plugin HookPlugin, options HookPluginOptions) (func(), error) {
	if s == nil || s.notifyHub == nil {
		return nil, fmt.Errorf("hook hub not initialized")
	}
	return s.notifyHub.RegisterPlugin(plugin, options)
}
