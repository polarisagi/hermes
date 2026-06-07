package translate

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/polarisagi/hermes/internal/domain"
)

// BuildTargetURL 组装目标后端 URL
func BuildTargetURL(provider *domain.UserProvider, targetEndpoint *domain.SysAccessEndpoint, incomingPath string) string {
	var baseURL string
	if targetEndpoint != nil {
		baseURL = strings.TrimSuffix(targetEndpoint.DefaultBaseURL, "/")
	}

	subPath := strings.TrimPrefix(incomingPath, "/v1")
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}

	// 特殊处理 Google Gemini (根据模型名称等可能需要 /v1beta)
	if strings.Contains(baseURL, "generativelanguage.googleapis") {
		if strings.Contains(subPath, "preview") || strings.Contains(subPath, "3.1") ||
			strings.Contains(subPath, "2.5") || strings.Contains(subPath, "2.0") ||
			strings.Contains(subPath, "lite") {
			if strings.HasSuffix(baseURL, "/v1") {
				baseURL = strings.TrimSuffix(baseURL, "/v1") + "/v1beta"
			} else if !strings.HasSuffix(baseURL, "/v1beta") {
				baseURL = baseURL + "/v1beta"
			}
		}
	}

	return baseURL + subPath
}

// ForwardStreamBody 将 body 流式转发到 w，维护尾部 8KB 缓冲窗口
func ForwardStreamBody(w http.ResponseWriter, body io.Reader) (tailBuf []byte, totalWritten int64) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 8192)
	const tailWindowSize = 8192

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
			totalWritten += int64(n)
			tailBuf = append(tailBuf, buf[:n]...)
			if len(tailBuf) > tailWindowSize {
				tailBuf = tailBuf[len(tailBuf)-tailWindowSize:]
			}
		}
		if readErr != nil {
			break
		}
	}
	return tailBuf, totalWritten
}

// ParseToInt 安全地将字节切片解析为 int64
func ParseToInt(b []byte) int64 {
	var n int64
	if _, err := fmt.Sscanf(string(b), "%d", &n); err != nil {
		return 0
	}
	return n
}

// CopyHeaders 安全复制请求头，忽略禁止转发的头
func CopyHeaders(dst http.Header, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Host") ||
			strings.EqualFold(k, "Content-Length") ||
			strings.EqualFold(k, "Transfer-Encoding") ||
			strings.EqualFold(k, "Accept-Encoding") ||
			strings.EqualFold(k, "Authorization") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
