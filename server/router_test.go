package server

import (
	"encoding/json"
	"fmt"
	config "lalmax/conf"
	"lalmax/hook"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/q191201771/lal/pkg/base"
)

var max *LalMaxServer

const httpNotifyAddr = ":55559"

func TestMain(m *testing.M) {
	var err error
	max, err = NewLalMaxServer(&config.Config{
		HttpFmp4Config:   config.HttpFmp4Config{Enable: true},
		LalSvrConfigPath: "../conf/lalserver.conf.json",
		HttpConfig: config.HttpConfig{
			ListenAddr: ":52349",
		},
		HttpNotifyConfig: config.HttpNotifyConfig{
			Enable:            true,
			UpdateIntervalSec: 2,
			OnUpdate:          fmt.Sprintf("http://127.0.0.1%s/on_update", httpNotifyAddr),
		},
	})
	if err != nil {
		panic(err)
	}
	go max.Run()
	os.Exit(m.Run())
}

func TestAllGroup(t *testing.T) {
	_, err := max.lalsvr.AddCustomizePubSession("test")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("no consumer", func(t *testing.T) {
		r := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/stat/all_group", nil)
		max.router.ServeHTTP(r, req)
		resp := r.Result()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
		var out base.ApiStatAllGroupResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if len(out.Data.Groups) <= 0 {
			t.Fatal("no group")
		}
		if len(out.Data.Groups[0].StatSubs) != 0 {
			t.Fatal("subs err")
		}
	})

	t.Run("has consumer", func(t *testing.T) {
		ss := hook.NewHookSession("test", "test", max.hlssvr)
		ss.AddConsumer("consumer1", nil)
		hook.GetHookSessionManagerInstance().SetHookSession("test", ss)

		r := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/stat/all_group", nil)
		max.router.ServeHTTP(r, req)
		resp := r.Result()
		if resp.StatusCode != 200 {
			t.Fatal(resp.Status)
		}
		var out base.ApiStatAllGroupResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if len(out.Data.Groups) <= 0 {
			t.Fatal("no group")
		}
		if len(out.Data.Groups[0].StatSubs) <= 0 {
			t.Fatal("subs err")
		}
		group := out.Data.Groups[0]
		if group.StatSubs[0].SessionId != "consumer1" {
			t.Fatal("SessionId err")
		}
	})
}

func TestNotifyUpdate(t *testing.T) {
	_, err := max.lalsvr.AddCustomizePubSession("test")
	if err != nil {
		t.Fatal(err)
	}
	ss := hook.NewHookSession("test", "test", max.hlssvr)
	ss.AddConsumer("consumer1", nil)
	hook.GetHookSessionManagerInstance().SetHookSession("test", ss)

	http.HandleFunc("/on_update", func(w http.ResponseWriter, r *http.Request) {
		var out base.ApiStatAllGroupResp
		if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if len(out.Data.Groups) <= 0 {
			t.Fatal("no group")
		}
		if len(out.Data.Groups[0].StatSubs) <= 0 {
			t.Fatal("subs err")
		}
		group := out.Data.Groups[0]
		if group.StatSubs[0].SessionId != "consumer1" {
			t.Fatal("SessionId err")
		}
	})
	go http.ListenAndServe(httpNotifyAddr, nil)
	time.Sleep(time.Second * 3)
}
