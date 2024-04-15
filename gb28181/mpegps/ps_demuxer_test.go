package mpegps

import (
	"encoding/hex"
	"fmt"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/rtprtcp"
	"github.com/q191201771/naza/pkg/nazabytes"
	"github.com/q191201771/naza/pkg/nazalog"
	"io"
	"os"
	"testing"
)

var ps1 []byte = []byte{0x00, 0x00, 0x01, 0xBA}
var ps2 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0xFF}

var ps3 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF0, 0x00, 0x00, 0x01, 0xBB}
var ps4 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0x34, 0x00, 0x00, 0x01, 0xBB, 0x00, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0x34}
var ps5 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00, 0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0x34, 0x00, 0x00, 0x01, 0xBB, 0x00, 0x09, 0x00, 0x01, 0x33, 0x44, 0xFF, 0x34, 0x81, 0x00, 0x00}
var ps6 []byte = []byte{0x00, 0x00, 0x01, 0xBC, 0x40, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x34, 0x81, 0x00, 0x00}
var ps7 []byte = []byte{0x00, 0x00, 0x01, 0xBA, 0x20, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}

func TestPSDemuxer_Input(t *testing.T) {
	type fields struct {
		streamMap map[uint8]*psstream
		pkg       *PSPacket
		cache     []byte
		OnPacket  func(pkg Display, decodeResult error)
		OnFrame   func(frame []byte, cid PS_STREAM_TYPE, pts uint64, dts uint64)
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{name: "test1", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps1}, wantErr: true},

		{name: "test2", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps2}, wantErr: false},

		{name: "test3", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps3}, wantErr: true},

		{name: "test4", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps4}, wantErr: true},

		{name: "test5", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps5}, wantErr: false},
		{name: "test6", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps6}, wantErr: false},
		{name: "test-mpeg1", fields: fields{
			streamMap: make(map[uint8]*psstream),
			pkg:       new(PSPacket),
		}, args: args{data: ps7}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			psdemuxer := &PSDemuxer{
				streamMap: tt.fields.streamMap,
				pkg:       tt.fields.pkg,
				cache:     tt.fields.cache,
				OnPacket:  tt.fields.OnPacket,
				OnFrame:   tt.fields.OnFrame,
			}
			if err := psdemuxer.Input(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("PSDemuxer.Input() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
func TestPSDemuxer(t *testing.T) {
	var psUnpacker *PSDemuxer
	dumpFile := base.NewDumpFile()
	err := dumpFile.OpenToRead("C:\\Users\\Administrator\\Desktop\\34132311401328010001.raw")
	if err != nil {
		fmt.Println(err)
		return
	}
	psUnpacker = NewPSDemuxer()
	psUnpacker.OnFrame = func(frame []byte, cid PS_STREAM_TYPE, pts uint64, dts uint64) {

	}
	fd3, err := os.OpenFile("ps_demux_result", os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer fd3.Close()
	psUnpacker.OnPacket = func(pkg Display, decodeResult error) {
		switch value := pkg.(type) {
		case *PSPackHeader:
			fd3.WriteString("--------------PS Pack Header--------------\n")
			if decodeResult == nil {
				value.PrettyPrint(fd3)
			} else {
				fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
			}
		case *System_header:
			fd3.WriteString("--------------System Header--------------\n")
			if decodeResult == nil {
				value.PrettyPrint(fd3)
			} else {
				fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
			}
		case *Program_stream_map:
			fd3.WriteString("--------------------PSM-------------------\n")
			if decodeResult == nil {
				value.PrettyPrint(fd3)
			} else {
				fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
			}
		case *PesPacket:
			fd3.WriteString("-------------------PES--------------------\n")
			if decodeResult == nil {
				value.PrettyPrint(fd3)
			} else {
				fd3.WriteString(fmt.Sprintf("Decode Ps Packet Failed %s\n", decodeResult.Error()))
			}
		}
	}

	if err != nil {
		return
	}
	dump := false
	packe := 0
	for {
		m, err := dumpFile.ReadOneMessage()
		if err == io.EOF {
			break
		}
		ipkt, err := rtprtcp.ParseRtpPacket(m.Body)
		if err != nil {
			nazalog.Errorf("PsUnpacker ParseRtpPacket failed. b=%s, err=%+v",
				hex.Dump(nazabytes.Prefix(m.Body, 64)), err)
			continue
		}
		packe++
		fmt.Printf("pkt Seq:%d ssrc:%d \n", ipkt.Header.Seq, ipkt.Header.Ssrc)
		body := ipkt.Body()
		if ipkt.Header.Mark == 1 {
			dump = true
			if packe == 1 {
				if len(body) > 8 {
					fmt.Println(hex.Dump(body[:8]))
				}
			}
		}
		if dump || packe > 1 {
			dump = false
			if len(body) > 8 {
				fmt.Println(hex.Dump(body[:8]))
			}
		}
		fmt.Println(psUnpacker.Input(body))
	}

}
