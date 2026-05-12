package server

// ZLM 兼容层请求/响应类型定义
// 为什么放在 server 包：ZLM 兼容路由与现有 lalmax 路由同级，需访问 LalMaxServer 内部成员

// ZlmFixedHeader ZLM 标准响应头
type ZlmFixedHeader struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
}

// --- /index/api/openRtpServer ---

type ZlmOpenRtpServerReq struct {
	Port     int    `json:"port"`
	TCPMode  int8   `json:"tcp_mode"`
	StreamID string `json:"stream_id"`
}

type ZlmOpenRtpServerResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Port int    `json:"port"`
}

// --- /index/api/closeRtpServer ---

type ZlmCloseRtpServerReq struct {
	StreamID string `json:"stream_id"`
}

type ZlmCloseRtpServerResp struct {
	Code int `json:"code"`
	Hit  int `json:"hit"`
}

// --- /index/api/close_streams ---

type ZlmCloseStreamsReq struct {
	Schema string `json:"schema,omitempty"`
	Vhost  string `json:"vhost,omitempty"`
	App    string `json:"app,omitempty"`
	Stream string `json:"stream,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

type ZlmCloseStreamsResp struct {
	Code        int `json:"code"`
	CountHit    int `json:"count_hit"`
	CountClosed int `json:"count_closed"`
}

// --- /index/api/getServerConfig ---

type ZlmGetServerConfigResp struct {
	Code int              `json:"code"`
	Data []map[string]any `json:"data"`
}

// --- /index/api/setServerConfig ---

type ZlmSetServerConfigResp struct {
	ZlmFixedHeader
	Changed int `json:"changed"`
}

// --- /index/api/startRecord ---

type ZlmStartRecordReq struct {
	Type       int    `json:"type"`
	Vhost      string `json:"vhost"`
	App        string `json:"app"`
	Stream     string `json:"stream"`
	CustomPath string `json:"customized_path,omitempty"`
	MaxSecond  int    `json:"max_second,omitempty"`
}

type ZlmStartRecordResp struct {
	ZlmFixedHeader
	Result bool `json:"result"`
}

// --- /index/api/stopRecord ---

type ZlmStopRecordReq struct {
	Type   int    `json:"type"`
	Vhost  string `json:"vhost"`
	App    string `json:"app"`
	Stream string `json:"stream"`
}

type ZlmStopRecordResp struct {
	ZlmFixedHeader
	Result bool `json:"result"`
}

// --- /index/api/addStreamProxy ---

type ZlmAddStreamProxyReq struct {
	Vhost      string  `json:"vhost"`
	App        string  `json:"app"`
	Stream     string  `json:"stream"`
	URL        string  `json:"url"`
	RetryCount int     `json:"retry_count"`
	RTPType    int     `json:"rtp_type"`
	TimeoutSec float32 `json:"timeout_sec"`
}

type ZlmAddStreamProxyResp struct {
	ZlmFixedHeader
	Data struct {
		Key string `json:"key"`
	} `json:"data"`
}

// --- /index/api/getSnap ---

type ZlmGetSnapReq struct {
	URL        string `json:"url"`
	TimeoutSec int    `json:"timeout_sec"`
	ExpireSec  int    `json:"expire_sec"`
}

// --- on_stream_changed Hook Payload ---

type ZlmOnStreamChangedPayload struct {
	Regist           bool              `json:"regist"`
	AliveSecond      int               `json:"aliveSecond"`
	App              string            `json:"app"`
	BytesSpeed       int               `json:"bytesSpeed"`
	CreateStamp      int64             `json:"createStamp"`
	MediaServerID    string            `json:"mediaServerId"`
	OriginSock       ZlmOriginSock     `json:"originSock"`
	OriginType       int               `json:"originType"`
	OriginTypeStr    string            `json:"originTypeStr"`
	OriginURL        string            `json:"originUrl"`
	ReaderCount      int               `json:"readerCount"`
	Schema           string            `json:"schema"`
	Stream           string            `json:"stream"`
	TotalReaderCount int               `json:"totalReaderCount"`
	Tracks           []ZlmTrack        `json:"tracks"`
	Vhost            string            `json:"vhost"`
	AppName          string            `json:"app_name,omitempty"`
	StreamName       string            `json:"stream_name,omitempty"`
}

type ZlmOriginSock struct {
	Identifier string `json:"identifier"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	PeerIP     string `json:"peer_ip"`
	PeerPort   int    `json:"peer_port"`
}

type ZlmTrack struct {
	Channels    int     `json:"channels,omitempty"`
	CodecID     int     `json:"codec_id"`
	CodecIDName string  `json:"codec_id_name"`
	CodecType   int     `json:"codec_type"`
	Ready       bool    `json:"ready"`
	SampleBit   int     `json:"sample_bit,omitempty"`
	SampleRate  int     `json:"sample_rate,omitempty"`
	Fps         float32 `json:"fps,omitempty"`
	Height      int     `json:"height,omitempty"`
	Width       int     `json:"width,omitempty"`
}

// --- on_server_keepalive Hook Payload ---

type ZlmOnServerKeepalivePayload struct {
	MediaServerID string `json:"mediaServerId"`
}

// --- on_stream_none_reader Hook Payload ---

type ZlmOnStreamNoneReaderPayload struct {
	MediaServerID string `json:"mediaServerId"`
	App           string `json:"app"`
	Schema        string `json:"schema"`
	Stream        string `json:"stream"`
	Vhost         string `json:"vhost"`
}

// --- on_record_mp4 Hook Payload ---

type ZlmOnRecordMp4Payload struct {
	MediaServerID string  `json:"mediaServerId"`
	App           string  `json:"app"`
	FileName      string  `json:"file_name"`
	FilePath      string  `json:"file_path"`
	FileSize      int64   `json:"file_size"`
	Folder        string  `json:"folder"`
	StartTime     int64   `json:"start_time"`
	Stream        string  `json:"stream"`
	TimeLen       float64 `json:"time_len"`
	URL           string  `json:"url"`
	Vhost         string  `json:"vhost"`
}

// --- on_publish Hook Payload ---

type ZlmOnPublishPayload struct {
	MediaServerID string `json:"mediaServerId"`
	App           string `json:"app"`
	ID            string `json:"id"`
	IP            string `json:"ip"`
	Params        string `json:"params"`
	Port          int    `json:"port"`
	Schema        string `json:"schema"`
	Stream        string `json:"stream"`
	Vhost         string `json:"vhost"`
}

// --- on_play Hook Payload ---

type ZlmOnPlayPayload struct {
	MediaServerID string `json:"mediaServerId"`
	App           string `json:"app"`
	ID            string `json:"id"`
	IP            string `json:"ip"`
	Params        string `json:"params"`
	Port          int    `json:"port"`
	Schema        string `json:"schema"`
	Stream        string `json:"stream"`
	Vhost         string `json:"vhost"`
}

// --- on_stream_not_found Hook Payload ---

type ZlmOnStreamNotFoundPayload struct {
	MediaServerID string `json:"mediaServerId"`
	App           string `json:"app"`
	ID            string `json:"id"`
	IP            string `json:"ip"`
	Params        string `json:"params"`
	Port          int    `json:"port"`
	Schema        string `json:"schema"`
	Stream        string `json:"stream"`
	Vhost         string `json:"vhost"`
	AppName       string `json:"app_name,omitempty"`
	StreamName    string `json:"stream_name,omitempty"`
}

// --- on_rtp_server_timeout Hook Payload ---

type ZlmOnRtpServerTimeoutPayload struct {
	LocalPort     int    `json:"local_port"`
	ReUsePort     bool   `json:"re_use_port"`
	SSRC          uint32 `json:"ssrc"`
	StreamID      string `json:"stream_id"`
	TCPMode       int    `json:"tcp_mode"`
	MediaServerID string `json:"mediaServerId"`
}
