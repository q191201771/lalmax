package srt

import (
	"testing"

	gosrt "github.com/datarhei/gosrt"
)

func TestGetStreamInfoMalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		"",
		"h=test,m=publish",
		"h",
		"#!::h",
		"#!::m",
		"#!::h=test,m",
		"#!::h=,m=publish",
		"#!::m=publish",
	}

	for _, streamID := range cases {
		t.Run(streamID, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("getStreamInfo panicked: %v", recovered)
				}
			}()
			info := getStreamInfo(streamID)
			if info.Mode != gosrt.REJECT {
				t.Fatalf("mode = %v", info.Mode)
			}
		})
	}
}

func TestGetStreamInfoPublish(t *testing.T) {
	info := getStreamInfo("#!::h=test110,m=publish")
	if info.StreamName != "test110" {
		t.Fatalf("stream name = %q", info.StreamName)
	}
	if info.Mode != gosrt.PUBLISH {
		t.Fatalf("mode = %v", info.Mode)
	}
}

func TestGetStreamInfoRequest(t *testing.T) {
	info := getStreamInfo("#!::h=test110,m=request")
	if info.StreamName != "test110" {
		t.Fatalf("stream name = %q", info.StreamName)
	}
	if info.Mode != gosrt.SUBSCRIBE {
		t.Fatalf("mode = %v", info.Mode)
	}
}
