package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// gcloud CLI default client ID and secret
const (
	clientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	clientSecret = "d-qshared-pb-base" // This is a public secret used for desktop apps
)

func main() {
	fmt.Println("==================================================")
	fmt.Println("  PolarisAGI Hermes - Google ADC 自动授权工具")
	fmt.Println("==================================================")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("无法绑定本地端口: %v\n", err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/generative-language.retriever",
		},
		Endpoint: google.Endpoint,
	}

	state := generateState()
	url := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Printf("\n正在打开浏览器...\n如果没有自动打开，请手动复制并在浏览器中访问以下链接：\n\n%s\n\n", url)
	fmt.Println("请在浏览器中登录并点击“允许”授权。正在等待回调...")

	openBrowser(url)

	codeChan := make(chan string)
	errChan := make(chan error)

	server := &http.Server{}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		queryError := r.URL.Query().Get("error")
		if queryError != "" {
			fmt.Fprintf(w, "授权失败: %s. 你可以关闭此窗口。", queryError)
			errChan <- fmt.Errorf("authorization error: %s", queryError)
			return
		}

		retState := r.URL.Query().Get("state")
		if retState != state {
			fmt.Fprintf(w, "State不匹配，可能存在安全风险. 你可以关闭此窗口。")
			errChan <- fmt.Errorf("invalid state")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Fprintf(w, "未找到授权码. 你可以关闭此窗口。")
			errChan <- fmt.Errorf("no code found")
			return
		}

		fmt.Fprintf(w, "<h1>授权成功！</h1><p>已获取授权码，你可以关闭此页面并返回控制台。</p>")
		codeChan <- code
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		fmt.Printf("\n获取授权码失败: %v\n", err)
		os.Exit(1)
	case <-time.After(5 * time.Minute):
		fmt.Printf("\n等待超时 (5分钟). 请重试.\n")
		os.Exit(1)
	}

	_ = server.Shutdown(context.Background())

	fmt.Println("\n已获得授权码，正在请求刷新令牌...")

	tok, err := conf.Exchange(context.Background(), code)
	if err != nil {
		fmt.Printf("获取 Token 失败: %v\n", err)
		os.Exit(1)
	}

	adc := map[string]interface{}{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"refresh_token": tok.RefreshToken,
		"type":          "authorized_user",
	}

	jsonBytes, err := json.MarshalIndent(adc, "", "  ")
	if err != nil {
		fmt.Printf("生成 JSON 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n==================================================")
	fmt.Println("✅ 授权成功！请将以下 JSON 内容完整复制，")
	fmt.Println("   粘贴到 PolarisAGI 后台节点的「Key / Token」输入框中。")
	fmt.Println("==================================================")
	fmt.Println(string(jsonBytes))
	fmt.Println("\n==================================================")
	fmt.Println("按回车键退出...")
	_, _ = fmt.Scanln()
}

func generateState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("无法打开浏览器: %v\n", err)
	}
}
