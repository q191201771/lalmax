package server

import (
	"context"
	"crypto/tls"
	config "lalmax/conf"
	"lalmax/fmp4/hls"
	httpfmp4 "lalmax/fmp4/http-fmp4"
	"lalmax/gb28181"
	"lalmax/hook"
	"lalmax/onvif"
	"lalmax/rtc"
	"lalmax/srt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type LalMaxServer struct {
	lalsvr      logic.ILalServer
	conf        *config.Config
	srtsvr      *srt.SrtServer
	rtcsvr      *rtc.RtcServer
	router      *gin.Engine
	routerTls   *gin.Engine
	httpfmp4svr *httpfmp4.HttpFmp4Server
	hlssvr      *hls.HlsServer
	gbsbr       *gb28181.GB28181Server
	onvifsvr    *onvif.OnvifServer
}

func NewLalMaxServer(conf *config.Config) (*LalMaxServer, error) {
	lalsvr := logic.NewLalServer(func(option *logic.Option) {
		option.ConfFilename = conf.LalSvrConfigPath
		option.NotifyHandler = NewHttpNotify(conf.HttpNotifyConfig, conf.ServerId)
	})

	maxsvr := &LalMaxServer{
		lalsvr: lalsvr,
		conf:   conf,
	}

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
	}

	if conf.HttpFmp4Config.Enable {
		maxsvr.httpfmp4svr = httpfmp4.NewHttpFmp4Server()
	}

	if conf.HlsConfig.Enable {
		maxsvr.hlssvr = hls.NewHlsServer(conf.HlsConfig)
	}

	if conf.GB28181Config.Enable {
		maxsvr.gbsbr = gb28181.NewGB28181Server(conf.GB28181Config, lalsvr)
	}

	if conf.OnvifConfig.Enable {
		maxsvr.onvifsvr = onvif.NewOnvifServer()
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
		// 有新的流了，创建业务层的对象，用于hook这个流
		return hook.NewHookSession(uniqueKey, streamName, s.hlssvr)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if s.srtsvr != nil {
		go s.srtsvr.Run(ctx)
	}

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

	if s.gbsbr != nil {
		go s.gbsbr.Start()
	}

	return s.lalsvr.RunLoop()
}
