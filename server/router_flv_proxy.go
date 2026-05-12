package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/naza/pkg/nazalog"
)

// initFlvProxy 注册 NoRoute 兜底，将 .flv 请求代理到 lal 的 httpflv 服务
// 为什么：ZLM 的 FLV 拉流路径是 /{app}/{stream}.live.flv，lal 的 httpflv 在独立端口，
// lalmax 不直接提供 httpflv，通过反向代理让外部只需访问 lalmax 单一端口
func (s *LalMaxServer) initFlvProxy(router *gin.Engine) {
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasSuffix(path, ".flv") {
			c.Status(http.StatusNotFound)
			return
		}

		lalHTTPAddr := s.getLalHttpflvAddr()
		if lalHTTPAddr == "" {
			c.Status(http.StatusBadGateway)
			return
		}

		targetURL := "http://" + lalHTTPAddr + path
		if c.Request.URL.RawQuery != "" {
			targetURL += "?" + c.Request.URL.RawQuery
		}

		nazalog.Debugf("flv proxy. path=%s, target=%s", path, targetURL)

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, targetURL, nil)
		if err != nil {
			nazalog.Errorf("flv proxy create request failed. err=%v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		for k, vs := range c.Request.Header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			nazalog.Errorf("flv proxy request failed. target=%s, err=%v", targetURL, err)
			c.Status(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vs := range resp.Header {
			for _, v := range vs {
				c.Header(k, v)
			}
		}
		c.Status(resp.StatusCode)

		if resp.StatusCode != http.StatusOK {
			return
		}

		c.Header("Transfer-Encoding", "chunked")
		c.Writer.Flush()
		io.Copy(c.Writer, resp.Body)
	})
}

// getLalHttpflvAddr 从 lal 原始配置中提取 httpflv 服务地址
func (s *LalMaxServer) getLalHttpflvAddr() string {
	if len(s.conf.LalRawContent) == 0 {
		return ""
	}

	var raw struct {
		DefaultHTTP struct {
			Addr string `json:"http_listen_addr"`
		} `json:"default_http"`
	}

	if err := json.Unmarshal(s.conf.LalRawContent, &raw); err != nil {
		return ""
	}

	addr := raw.DefaultHTTP.Addr
	if addr == "" {
		return ""
	}
	if addr[0] == ':' {
		return "127.0.0.1" + addr
	}
	return addr
}
