package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
)

func (s *LalMaxServer) Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		c.Header("Access-Control-Allow-Origin", "*")
		//服务器支持的所有跨域请求的方法
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE,UPDATE")
		//允许跨域设置可以返回其他子段，可以自定义字段
		c.Header("Access-Control-Allow-Headers", "*")
		// 允许浏览器（客户端）可以解析的头部 （重要）
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
		//设置缓存时间
		c.Header("Access-Control-Max-Age", "172800")
		//允许客户端传递校验信息比如 cookie (重要)
		c.Header("Access-Control-Allow-Credentials", "true")

		//允许类型校验
		if method == "OPTIONS" {
			c.Status(http.StatusOK)
		}
		c.Next()
	}
}

// Authentication 接口鉴权
func Authentication(token string, acceptIPs []string) gin.HandlerFunc {
	out := base.ApiRespBasic{
		ErrorCode: http.StatusUnauthorized,
		Desp:      http.StatusText(http.StatusUnauthorized),
	}
	return func(c *gin.Context) {
		if !authentication(c.Query("token"), token, c.ClientIP(), acceptIPs) {
			c.JSON(200, out)
			return
		}
		c.Next()
	}
}

// authentication 判断是否符合要求，返回 false 表示鉴权失败
func authentication(reqToken, svcToken, clientIP string, acceptIPs []string) bool {
	// token 鉴权失败
	if svcToken != "" && reqToken != svcToken {
		return false
	}
	// ip 白名单过滤
	if len(acceptIPs) > 0 && !containFn(acceptIPs, clientIP) {
		return false
	}
	return true
}

func containFn[T comparable](ts []T, t T) bool {
	for _, v := range ts {
		if v == t {
			return true
		}
	}
	return false
}
