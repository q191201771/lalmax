package srt

import "testing"

func TestAACSampleRateFromIndex(t *testing.T) {
	rate, ok := aacSampleRateFromIndex(4)
	if !ok || rate != 44100 {
		t.Fatalf("index 4 = %d ok=%v", rate, ok)
	}

	rate, ok = aacSampleRateFromIndex(15)
	if ok || rate != 0 {
		t.Fatalf("reserved index = %d ok=%v", rate, ok)
	}

	rate, ok = aacSampleRateFromIndex(-1)
	if ok || rate != 0 {
		t.Fatalf("negative index = %d ok=%v", rate, ok)
	}
}

func TestAACFrameDurationMsRejectsZeroRate(t *testing.T) {
	if _, ok := aacFrameDurationMs(0); ok {
		t.Fatal("zero sample rate was accepted")
	}

	duration, ok := aacFrameDurationMs(44100)
	if !ok || duration == 0 {
		t.Fatalf("duration = %d ok=%v", duration, ok)
	}
}

func TestADTSFrameBounds(t *testing.T) {
	frame := makeADTSFrame(10, false)
	headerLen, frameLen, ok := adtsFrameBounds(frame)
	if !ok || headerLen != 7 || frameLen != len(frame) {
		t.Fatalf("headerLen=%d frameLen=%d ok=%v", headerLen, frameLen, ok)
	}

	crcFrame := makeADTSFrame(12, true)
	headerLen, frameLen, ok = adtsFrameBounds(crcFrame)
	if !ok || headerLen != 9 || frameLen != len(crcFrame) {
		t.Fatalf("CRC headerLen=%d frameLen=%d ok=%v", headerLen, frameLen, ok)
	}
}

func TestADTSFrameBoundsRejectsMalformedFrame(t *testing.T) {
	tests := [][]byte{
		{0xFF, 0xF1},
		makeADTSFrame(20, false)[:10],
		makeADTSFrame(6, false),
		{0x00, 0xF1, 0, 0, 0, 0, 0},
	}
	for i, frame := range tests {
		if _, _, ok := adtsFrameBounds(frame); ok {
			t.Fatalf("case %d was accepted", i)
		}
	}
}

func makeADTSFrame(frameLen int, withCRC bool) []byte {
	bufferLen := frameLen
	if bufferLen < 7 {
		bufferLen = 7
	}
	frame := make([]byte, bufferLen)
	frame[0] = 0xFF
	frame[1] = 0xF1
	if withCRC {
		frame[1] = 0xF0
	}
	frame[2] = 0x50
	frame[3] = 0x80 | byte(frameLen>>11)
	frame[4] = byte(frameLen >> 3)
	frame[5] = byte(frameLen&0x07)<<5 | 0x1F
	frame[6] = 0xFC
	return frame
}
