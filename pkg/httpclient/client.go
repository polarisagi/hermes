package httpclient

import (
	"net"
	"net/http"
	"time"
)

// SharedTransport 全局共享的 HTTP Transport，优化了高并发和长连接代理场景
var SharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment, // 修复：支持系统 HTTP_PROXY/HTTPS_PROXY 环境变量
	MaxIdleConns:          1000,                      // 优化：提升总闲置连接数，防止多厂商场景下连接池抖动
	MaxIdleConnsPerHost:   100,                       // 优化：提升单 Host 闲置连接池上限
	MaxConnsPerHost:       0,                         // 0 = 不限并发连接，仅 idle 池有上限
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 600 * time.Second, // 等待首个响应头的时间（适配慢推理模型）
	ForceAttemptHTTP2:     true,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
}

// Client 全局统一的 HTTP 客户端
var Client = &http.Client{
	// 修复：移除全局的 Timeout 限制。
	// http.Client.Timeout 会限制整个请求（包括读取完整流数据）的生命周期。
	// 大模型的流式响应（如 o1 等深度推理模型）输出时间极易超过 10 分钟。
	// 设定硬性 Timeout 会导致长流被意外截断。整体超时应通过请求传入的 ctx 来控制。
	Timeout:   0,
	Transport: SharedTransport,
}
