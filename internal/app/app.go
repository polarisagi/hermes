package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/polarisagi/hermes/internal/api"
	"github.com/polarisagi/hermes/internal/clientcfg"
	"github.com/polarisagi/hermes/internal/config"
	"github.com/polarisagi/hermes/internal/pool"
	"github.com/polarisagi/hermes/internal/proxy"
	"github.com/polarisagi/hermes/internal/router"
	"github.com/polarisagi/hermes/internal/store"
	"github.com/polarisagi/hermes/internal/translate"
	anthropic2anthropic "github.com/polarisagi/hermes/internal/translate/anthropic/toanthropic"
	anthropic2deepseek "github.com/polarisagi/hermes/internal/translate/anthropic/todeepseek"
	anthropic2google "github.com/polarisagi/hermes/internal/translate/anthropic/togoogle"
	anthropic2openai "github.com/polarisagi/hermes/internal/translate/anthropic/toopenai"
	google2anthropic "github.com/polarisagi/hermes/internal/translate/google/toanthropic"
	google2google "github.com/polarisagi/hermes/internal/translate/google/togoogle"
	google2openai "github.com/polarisagi/hermes/internal/translate/google/toopenai"
	openai2anthropic "github.com/polarisagi/hermes/internal/translate/openai/toanthropic"
	openai2google "github.com/polarisagi/hermes/internal/translate/openai/togoogle"
	openai2openai "github.com/polarisagi/hermes/internal/translate/openai/toopenai"
	"github.com/polarisagi/hermes/pkg/logger"
	"github.com/polarisagi/hermes/web"
)

// App 封装了整个应用的依赖图与生命周期
type App struct{}

func New() *App {
	return &App{}
}

func (a *App) Run() error {
	// 1. 加载配置
	if err := config.LoadConfig("config.toml"); err != nil {
		slog.Error("加载配置失败", "error", err)
		return err
	}

	// 2. 初始化日志
	logger.InitLogger()

	// 3. 初始化数据库
	store.InitDB()

	// 4. 初始化 Store 层
	providerRepo := store.NewProviderRepo()
	modelRepo := store.NewModelRepo()
	routeRepo := store.NewRouteRepo()
	intentRepo := store.NewIntentRepo()
	routingLogRepo := store.NewRoutingLogRepo()
	extCacheRepo := store.NewExternalCacheRepo()
	settingsRepo := store.NewSettingsRepo(store.DB())
	clientBackupRepo := store.NewClientBackupRepo(store.DB())

	// 5. 初始化核心服务
	intentInferer := router.NewIntentInferer(intentRepo)
	chanManager := pool.NewManager(providerRepo, modelRepo)

	if err := chanManager.Reload(context.Background()); err != nil {
		slog.Error("渠道初始加载失败", "error", err)
	}

	pipeline := router.NewPipeline(routeRepo, intentRepo, intentInferer, chanManager, routingLogRepo)
	if err := pipeline.Reload(context.Background()); err != nil {
		slog.Warn("路由管线缓存预热失败，将降级为实时查询", "error", err)
	}

	// 路由日志定期清理（保留 7 天）
	go func() {
		_ = routingLogRepo.PruneOldLogs(context.Background(), 7)
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			_ = routingLogRepo.PruneOldLogs(context.Background(), 7)
		}
	}()

	// 6. 注册协议转换器
	transFactory := translate.NewFactory()

	// OpenAI 入站
	transFactory.Register("openai_openai", openai2openai.NewTranslator())
	transFactory.Register("openai_local", openai2openai.NewTranslator())
	transFactory.Register("openai_anthropic", openai2anthropic.NewTranslator())
	transFactory.Register("openai_google", openai2google.NewTranslator())

	// Anthropic 入站
	transFactory.Register("anthropic_google", anthropic2google.NewTranslator())
	transFactory.Register("anthropic_openai", anthropic2openai.NewTranslator())
	transFactory.Register("anthropic_anthropic", anthropic2anthropic.NewTranslator())
	transFactory.Register("anthropic_deepseek", anthropic2deepseek.NewTranslator())

	// Google 入站
	transFactory.Register("google_google", google2google.NewTranslator())
	transFactory.Register("google_openai", google2openai.NewTranslator())
	transFactory.Register("google_anthropic", google2anthropic.NewTranslator())

	// 7. 数据面代理
	proxyServer := proxy.NewServer(pipeline, chanManager, transFactory)

	// 8. 控制面 API
	clientSvc := clientcfg.NewManager(settingsRepo, clientBackupRepo)
	adminHandler := api.NewAdminHandler(
		providerRepo, modelRepo, routeRepo, intentRepo,
		settingsRepo, extCacheRepo, clientSvc, chanManager, pipeline,
	)

	// 9. 路由挂载
	mux := http.NewServeMux()
	adminHandler.RegisterRoutes(mux)

	uiFS, err := fs.Sub(web.FS, "ui")
	if err != nil {
		slog.Error("加载嵌入式前端静态资源失败", "error", err)
	} else {
		mux.Handle("/", http.FileServer(http.FS(uiFS)))
	}

	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	mux.Handle("/v1/", proxyServer)

	// 10. 启动
	listenAddr := config.GlobalConfig.Server.ListenAddr
	slog.Info("PolarisAGI Hermes 启动中", "addr", listenAddr)

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("监听端口失败", "addr", listenAddr, "error", err)
		os.Exit(1)
	}
	// 端口已绑定成功后再尝试打开浏览器，避免出现"无法访问该网站"的瞬时错误
	maybeOpenDashboard(context.Background(), settingsRepo, listenAddr)

	if err := http.Serve(ln, mux); err != nil {
		slog.Error("服务器崩溃", "error", err)
		os.Exit(1)
	}
	return nil
}
