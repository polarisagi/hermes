package billing

import (
	"bytes"
	"net/http"
	"regexp"
	"strconv"
)

var (
	OpenAIPromptRegex     = regexp.MustCompile(`"prompt_tokens"\s*:\s*(\d+)`)
	OpenAICompletionRegex = regexp.MustCompile(`"completion_tokens"\s*:\s*(\d+)`)
	OpenAICachedRegex     = regexp.MustCompile(`"cached_tokens"\s*:\s*(\d+)`)

	PromptRegex        = regexp.MustCompile(`"promptTokenCount":\s*(\d+)`)
	CandidateRegex     = regexp.MustCompile(`"candidatesTokenCount":\s*(\d+)`)
	CachedContentRegex = regexp.MustCompile(`"cachedContentTokenCount":\s*(\d+)`)

	AnthropicInputRegex     = regexp.MustCompile(`"input_tokens"\s*:\s*(\d+)`)
	AnthropicOutputRegex    = regexp.MustCompile(`"output_tokens"\s*:\s*(\d+)`)
	AnthropicCacheReadRegex = regexp.MustCompile(`"cache_read_input_tokens"\s*:\s*(\d+)`)
)

func ParseToInt(b []byte) int64 {
	v, _ := strconv.ParseInt(string(b), 10, 64)
	return v
}

// ParseUsageFromStreamTail 从响应流尾部缓冲提取 token 用量，返回 prompt, completion, cached, 是否找到
func ParseUsageFromStreamTail(tailBuf []byte) (prompt, completion, cached int64, found bool) {
	// OpenAI
	if bytes.Contains(tailBuf, []byte("prompt_tokens")) || bytes.Contains(tailBuf, []byte("completion_tokens")) {
		pMatch := OpenAIPromptRegex.FindSubmatch(tailBuf)
		cMatch := OpenAICompletionRegex.FindSubmatch(tailBuf)
		cacheMatch := OpenAICachedRegex.FindSubmatch(tailBuf)
		if len(pMatch) > 1 {
			prompt = ParseToInt(pMatch[1])
		}
		if len(cMatch) > 1 {
			completion = ParseToInt(cMatch[1])
		}
		if len(cacheMatch) > 1 {
			cached = ParseToInt(cacheMatch[1])
		}
		if prompt > 0 || completion > 0 {
			return prompt, completion, cached, true
		}
	}

	// Vertex / Google
	if bytes.Contains(tailBuf, []byte("promptTokenCount")) || bytes.Contains(tailBuf, []byte("candidatesTokenCount")) {
		pMatch := PromptRegex.FindSubmatch(tailBuf)
		cMatch := CandidateRegex.FindSubmatch(tailBuf)
		cacheMatch := CachedContentRegex.FindSubmatch(tailBuf)
		if len(pMatch) > 1 {
			prompt = ParseToInt(pMatch[1])
		}
		if len(cMatch) > 1 {
			completion = ParseToInt(cMatch[1])
		}
		if len(cacheMatch) > 1 {
			cached = ParseToInt(cacheMatch[1])
		}
		if prompt > 0 || completion > 0 {
			return prompt, completion, cached, true
		}
	}

	// Anthropic
	if bytes.Contains(tailBuf, []byte("input_tokens")) || bytes.Contains(tailBuf, []byte("output_tokens")) {
		pMatch := AnthropicInputRegex.FindSubmatch(tailBuf)
		cMatch := AnthropicOutputRegex.FindSubmatch(tailBuf)
		cacheMatch := AnthropicCacheReadRegex.FindSubmatch(tailBuf)
		if len(pMatch) > 1 {
			prompt = ParseToInt(pMatch[1])
		}
		if len(cMatch) > 1 {
			completion = ParseToInt(cMatch[1])
		}
		if len(cacheMatch) > 1 {
			cached = ParseToInt(cacheMatch[1])
		}
		if prompt > 0 || completion > 0 {
			return prompt, completion, cached, true
		}
	}

	return 0, 0, 0, false
}

// InterceptResponseWriter wraps http.ResponseWriter to capture response body (or its tail) and status code.
type InterceptResponseWriter struct {
	http.ResponseWriter
	StatusCode  int
	BodyBuffer  *bytes.Buffer
	tailBuffer  []byte
	tailMaxSize int
	wroteHeader bool
}

func NewInterceptResponseWriter(w http.ResponseWriter) *InterceptResponseWriter {
	return &InterceptResponseWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK, // Default to 200
		BodyBuffer:     &bytes.Buffer{},
		tailMaxSize:    2048, // Capture the last 2KB for token extraction
		tailBuffer:     make([]byte, 0, 2048),
	}
}

func (w *InterceptResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.StatusCode = statusCode
		w.ResponseWriter.WriteHeader(statusCode)
		w.wroteHeader = true
	}
}

func (w *InterceptResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	// Buffer the entire body if it's small, or keep the tail for SSE parsing
	if w.BodyBuffer.Len() < 1024*1024 { // Up to 1MB
		w.BodyBuffer.Write(b)
	}

	// Update tail buffer
	newLen := len(w.tailBuffer) + len(b)
	if newLen > w.tailMaxSize {
		keep := w.tailMaxSize - len(b)
		if keep > 0 {
			w.tailBuffer = append(w.tailBuffer[len(w.tailBuffer)-keep:], b...)
		} else {
			w.tailBuffer = b[len(b)-w.tailMaxSize:]
		}
	} else {
		w.tailBuffer = append(w.tailBuffer, b...)
	}

	return w.ResponseWriter.Write(b)
}

func (w *InterceptResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *InterceptResponseWriter) GetTokens() (prompt, completion, cached int64, content string) {
	p, c, cache, found := ParseUsageFromStreamTail(w.tailBuffer)
	if !found {
		p, c, cache, _ = ParseUsageFromStreamTail(w.BodyBuffer.Bytes())
	}
	return p, c, cache, w.BodyBuffer.String()
}
