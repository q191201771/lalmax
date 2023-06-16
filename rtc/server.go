package rtc

import (
	"crypto/tls"
	"io"
	config "lalmax/conf"
	"net"
	"net/http"
	"strings"

	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type RtcServer struct {
	config    config.RtcConfig
	lalServer logic.ILalServer
	udpMux    ice.UDPMux
	tcpMux    ice.TCPMux
}

func NewRtcServer(config config.RtcConfig, lal logic.ILalServer) *RtcServer {
	return &RtcServer{
		config:    config,
		lalServer: lal,
	}
}

func (s *RtcServer) Run() (err error) {
	if s.config.ICEUDPMuxPort != 0 {
		var udplistener *net.UDPConn
		udplistener, err = net.ListenUDP("udp", &net.UDPAddr{
			IP:   net.IP{0, 0, 0, 0},
			Port: s.config.ICEUDPMuxPort,
		})

		if err != nil {
			nazalog.Error(err)
			return
		}

		s.udpMux = webrtc.NewICEUDPMux(nil, udplistener)
	}

	if s.config.ICETCPMuxPort != 0 {
		var tcplistener *net.TCPListener
		tcplistener, err = net.ListenTCP("tcp", &net.TCPAddr{
			IP:   net.IP{0, 0, 0, 0},
			Port: s.config.ICETCPMuxPort,
		})

		if err != nil {
			nazalog.Error(err)
			return
		}

		s.tcpMux = webrtc.NewICETCPMux(nil, tcplistener, 20)
	}
	if s.config.EnableHttps {
		go func() {
			s.serveWithTLS(s.config.HttpsListenAddr, s.config.HttpsCertFile, s.config.HttpsKeyFile)
		}()
	}
	return s.serveWithNet(s.config.HttpListenAddr)

}
func (s *RtcServer) serveWithNet(httpListenAddr string) error {
	listener, err := net.Listen("tcp", httpListenAddr)
	if err != nil {
		nazalog.Errorf("start webrtc http  listen failed.  err=%+v", err)
		return err
	}
	nazalog.Infof("start webrtc http server listen. addr=%s", httpListenAddr)
	httpSvr := http.Server{
		Addr:    httpListenAddr,
		Handler: http.HandlerFunc(s.ServeHttp),
	}
	err = httpSvr.Serve(listener)
	if err != nil {
		nazalog.Errorf("webrtc http  Serve failed.  err=%+v", err)
		return err
	}
	return nil
}
func (s *RtcServer) serveWithTLS(httpsListenAddr, certFile, keyFile string) {
	var cert tls.Certificate
	var err error
	cert, err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		nazalog.Errorf("start webrtc https listen failed. certFile=%s, keyFile=%s, err=%+v", certFile, keyFile, err)
		return
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	listeners, err := tls.Listen("tcp", httpsListenAddr, tlsConfig)
	if err != nil {
		nazalog.Errorf("start webrtc https listen failed.  err=%+v", err)
		return
	}
	nazalog.Infof("start webrtc https server listen. addr=%s", httpsListenAddr)
	httpSvrs := http.Server{
		Addr:    httpsListenAddr,
		Handler: http.HandlerFunc(s.ServeHttp),
	}
	err = httpSvrs.Serve(listeners)
	if err != nil {
		nazalog.Errorf("webrtc https Serve failed.  err=%+v", err)
		return
	}
}
func (s *RtcServer) ServeHttp(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Headers", "*")
	w.Header().Add("Access-Control-Allow-Methods", "*")
	w.Header().Add("Connection", "Close")

	switch r.Method {
	case http.MethodOptions:
		return
	case http.MethodPost:
		s.servePost(w, r)
	default:
		// 暂时只支持POST
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

func (s *RtcServer) servePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		nazalog.Error(err)
		return
	}

	if len(body) == 0 {
		nazalog.Error("invalid body")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	streamid := r.URL.Query().Get("streamid")
	if streamid == "" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if strings.HasSuffix(r.URL.Path, "/whip") {
		// whip
		s.handleWHIP(w, r, streamid, string(body))
	} else if strings.HasSuffix(r.URL.Path, "/whep") {
		// whep
		s.handleWHEP(w, r, streamid, string(body))
	}
}

func (s *RtcServer) handleWHIP(w http.ResponseWriter, r *http.Request, streamid, body string) {

	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	whipsession := NewWhipSession(streamid, pc, s.lalServer)
	if whipsession == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sdp := whipsession.GetAnswerSDP(string(body))
	if sdp == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go whipsession.Run()

	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(sdp))
}

func (s *RtcServer) handleWHEP(w http.ResponseWriter, r *http.Request, streamid, body string) {
	pc, err := newPeerConnection(s.config.ICEHostNATToIPs, s.udpMux, s.tcpMux)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	whepsession := NewWhepSession(streamid, pc, s.lalServer)
	if whepsession == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sdp := whepsession.GetAnswerSDP(string(body))
	if sdp == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	go whepsession.Run()

	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(sdp))
}
