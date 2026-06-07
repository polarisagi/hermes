// 前端 UI 静态资源嵌入
// 使用 Go embed 将 web/ui/ 目录下的 HTML/JS/CSS 文件编译进二进制
package web

import "embed"

//go:embed ui/*
var FS embed.FS // 嵌入的 UI 文件系统
