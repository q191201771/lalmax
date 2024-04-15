package mpegps

import "errors"

const (
	PsPackStartCodePackHeader       = 0x01ba
	PsPackStartCodeSystemHeader     = 0x01bb
	PsPackStartCodeProgramStreamMap = 0x01bc
	PsPackStartCodeAudioStream      = 0x01c0
	PsPackStartCodeVideoStream      = 0x01e0
	PsPackStartCodeHikStream        = 0x01bd

	PsPackStartCodePesPsd      = 0x01ff // program_stream_directory
	PsPackStartCodePesPadding  = 0x01be // padding_stream
	PsPackStartCodePesPrivate2 = 0x01bf // padding_stream_2
	PsPackStartCodePesEcm      = 0x01f0 // ECM_stream
	PsPackStartCodePesEmm      = 0x01f1 // EMM_stream

	PsPackStartCodePackEnd = 0x01b9
)

const (
	StreamTypeH264    uint8 = 0x1b
	StreamTypeH265          = 0x24
	StreamTypeAAC           = 0x0f
	StreamTypeG711A         = 0x90 //PCMA
	StreamTypeG7221         = 0x92
	StreamTypeG7231         = 0x93
	StreamTypeG729          = 0x99
	StreamTypeUnknown       = 0
)

const psbufInitSize = 4096
const (
	PsHeaderlen     int = 14
	SysHeaderlen    int = 18
	SysMapHeaderLen int = 24
	PesHeaderLen    int = 19
)
const (
	MaxPesLen        = 0xFFFF                       // 64k pes data
	MaxPesPayloadLen = MaxPesLen - PesHeaderLen + 5 // 64k pes data
)
const (
	StreamIdVideo = 0xe0
	StreamIdAudio = 0xc0
)
const adtsMinLen = 7
const naluStartCodeLen = 4

var maxUnpackRtpListSize = 2048
var ErrGb28181 = errors.New("tops.gb28181: fxxk")
