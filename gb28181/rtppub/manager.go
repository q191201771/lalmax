package rtppub

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	udpTransport "github.com/pion/transport/v3/udp"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/lalmax/config"
	"github.com/q191201771/lalmax/gb28181/mediaserver"
	"github.com/q191201771/naza/pkg/nazalog"
)

const (
	defaultPortMin          = 30000
	defaultPortMaxIncrement = 3000
)

var (
	errDuplicateStream = errors.New("rtp pub stream already exists")
	errSessionNotFound = errors.New("rtp pub session not found")
)

type Manager struct {
	mu sync.Mutex

	lalServer logic.ILalServer
	portMin   int
	portMax   int

	sessionsByID     map[string]*Session
	sessionsByStream map[string]*Session
	sessionsByKey    map[string]*Session
}

type Session struct {
	ID         string
	StreamName string
	MediaKey   string
	Network    string
	Port       int

	mediaInfo  mediaserver.MediaInfo
	server     *mediaserver.GB28181MediaServer
	lastActive time.Time
	done       chan struct{}
	closeOnce  sync.Once
}

func NewManager(lalServer logic.ILalServer, mediaConfig config.GB28181MediaConfig) *Manager {
	basePort := int(mediaConfig.ListenPort)
	if basePort == 0 {
		basePort = defaultPortMin
	}
	maxIncrement := mediaConfig.MultiPortMaxIncrement
	if maxIncrement == 0 {
		maxIncrement = defaultPortMaxIncrement
	}

	portMin := basePort
	if mediaConfig.ListenPort != 0 {
		portMin++
	}

	return &Manager{
		lalServer:        lalServer,
		portMin:          portMin,
		portMax:          basePort + int(maxIncrement),
		sessionsByID:     make(map[string]*Session),
		sessionsByStream: make(map[string]*Session),
		sessionsByKey:    make(map[string]*Session),
	}
}

func (m *Manager) Start(req base.ApiCtrlStartRtpPubReq) (ret base.ApiCtrlStartRtpPubResp) {
	if req.StreamName == "" {
		ret.ErrorCode = base.ErrorCodeParamMissing
		ret.Desp = base.DespParamMissing
		return
	}

	network := "udp"
	if req.IsTcpFlag != 0 {
		network = "tcp"
	}

	m.mu.Lock()
	if _, ok := m.sessionsByStream[req.StreamName]; ok {
		m.mu.Unlock()
		ret.ErrorCode = base.ErrorCodeListenUdpPortFail
		ret.Desp = errDuplicateStream.Error()
		return
	}
	m.mu.Unlock()

	listener, port, err := m.listen(req.Port, network)
	if err != nil {
		ret.ErrorCode = base.ErrorCodeListenUdpPortFail
		ret.Desp = err.Error()
		return
	}

	sessionID := base.GenUkPsPubSession()
	mediaKey := fmt.Sprintf("%s%d", network, port)
	session := &Session{
		ID:         sessionID,
		StreamName: req.StreamName,
		MediaKey:   mediaKey,
		Network:    network,
		Port:       port,
		mediaInfo: mediaserver.MediaInfo{
			StreamName:   req.StreamName,
			DumpFileName: req.DebugDumpPacket,
			MediaKey:     mediaKey,
		},
		lastActive: time.Now(),
		done:       make(chan struct{}),
	}
	readTimeout := time.Duration(req.TimeoutMs) * time.Millisecond
	session.server = mediaserver.NewGB28181MediaServer(port, mediaKey, m, m.lalServer).
		WithPreferMediaKeyLookup(true).
		WithReadTimeout(readTimeout)

	m.mu.Lock()
	if _, ok := m.sessionsByStream[req.StreamName]; ok {
		m.mu.Unlock()
		_ = listener.Close()
		ret.ErrorCode = base.ErrorCodeListenUdpPortFail
		ret.Desp = errDuplicateStream.Error()
		return
	}
	m.sessionsByID[session.ID] = session
	m.sessionsByStream[session.StreamName] = session
	m.sessionsByKey[session.MediaKey] = session
	m.mu.Unlock()

	if err = session.server.Start(listener); err != nil {
		m.stopSession(session)
		ret.ErrorCode = base.ErrorCodeListenUdpPortFail
		ret.Desp = err.Error()
		return
	}

	if req.TimeoutMs > 0 {
		go m.watchTimeout(session, time.Duration(req.TimeoutMs)*time.Millisecond)
	}

	ret.ErrorCode = base.ErrorCodeSucc
	ret.Desp = base.DespSucc
	ret.Data.SessionId = session.ID
	ret.Data.StreamName = session.StreamName
	ret.Data.Port = session.Port
	return
}

func (m *Manager) Stop(streamName, sessionID string) (*Session, error) {
	m.mu.Lock()
	var session *Session
	if sessionID != "" {
		session = m.sessionsByID[sessionID]
	} else if streamName != "" {
		session = m.sessionsByStream[streamName]
	}
	m.mu.Unlock()

	if session == nil {
		return nil, errSessionNotFound
	}

	m.stopSession(session)
	return session, nil
}

func (m *Manager) GetMediaInfoByKey(key string) (*mediaserver.MediaInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessionsByKey[key]
	if !ok {
		return nil, false
	}
	return &session.mediaInfo, true
}

func (m *Manager) CheckSsrc(ssrc uint32) (*mediaserver.MediaInfo, bool) {
	return nil, false
}

func (m *Manager) NotifyClose(streamName string) {
}

// UpdatePortRange 动态更新端口范围，由 setServerConfig 接口调用
// 为什么：owl 通过 setServerConfig 下发 rtp_proxy.port_range，需运行时生效
func (m *Manager) UpdatePortRange(portMin, portMax int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.portMin = portMin
	m.portMax = portMax
	nazalog.Infof("rtp pub port range updated. min=%d, max=%d", portMin, portMax)
}

func (m *Manager) OnRtpPacket(streamName string, mediaKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessionsByKey[mediaKey]; ok {
		session.lastActive = time.Now()
	}
}

func (m *Manager) stopSession(session *Session) {
	m.mu.Lock()
	if current := m.sessionsByID[session.ID]; current != session {
		m.mu.Unlock()
		return
	}
	delete(m.sessionsByID, session.ID)
	delete(m.sessionsByStream, session.StreamName)
	delete(m.sessionsByKey, session.MediaKey)
	session.closeOnce.Do(func() {
		close(session.done)
	})
	m.mu.Unlock()

	session.server.Dispose()
}

func (m *Manager) watchTimeout(session *Session, timeout time.Duration) {
	interval := timeout / 2
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-session.done:
			return
		case <-ticker.C:
			m.mu.Lock()
			current := m.sessionsByID[session.ID]
			expired := current == session && time.Since(session.lastActive) >= timeout
			m.mu.Unlock()
			if expired {
				nazalog.Warnf("rtp pub timeout, streamName:%s, sessionId:%s", session.StreamName, session.ID)
				m.stopSession(session)
				return
			}
		}
	}
}

func (m *Manager) listen(port int, network string) (net.Listener, int, error) {
	if port > 0 {
		listener, err := listenPort(port, network)
		return listener, port, err
	}

	var lastErr error
	for p := m.portMin; p <= m.portMax; p++ {
		listener, err := listenPort(p, network)
		if err == nil {
			return listener, p, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available %s port in range [%d,%d]", network, m.portMin, m.portMax)
	}
	return nil, 0, lastErr
}

func listenPort(port int, network string) (net.Listener, error) {
	addr := fmt.Sprintf(":%d", port)
	if network == "tcp" {
		return net.Listen("tcp", addr)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	return udpTransport.Listen("udp", udpAddr)
}
