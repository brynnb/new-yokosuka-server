package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/activitylog"
	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/discordchat"
	"github.com/brynnb/new-yokosuka-server/internal/httpapi"
	"github.com/brynnb/new-yokosuka-server/internal/logbuffer"
	"github.com/brynnb/new-yokosuka-server/internal/npc"
	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/realtime"
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptruntime"
	"github.com/brynnb/new-yokosuka-server/internal/store"
	"github.com/brynnb/new-yokosuka-server/internal/worldstate"
)

func main() {
	startedAt := time.Now()
	logLines := logbuffer.New(500)
	logger := log.New(io.MultiWriter(os.Stdout, logLines), "[new-yokosuka] ", log.LstdFlags|log.LUTC)
	log.SetOutput(io.MultiWriter(os.Stderr, logLines))
	database, err := store.Open(context.Background(), stringEnv("DATABASE_URL", ""))
	if err != nil {
		logger.Fatalf("database startup failed: %v", err)
	}
	defer database.Close()
	var scriptEngine *scriptevent.Engine
	var scriptPreview *scriptevent.PreviewRunner
	if compilerPath := stringEnv("YARN_COMPILER_PATH", ""); compilerPath != "" {
		compiler, err := scriptcontent.NewProcessCompiler(compilerPath)
		if err != nil {
			logger.Fatalf("Yarn compiler configuration failed: %v", err)
		}
		database.SetScriptCompiler(compiler)
		bridge, err := scriptruntime.NewBridge(compilerPath)
		if err != nil {
			logger.Fatalf("Yarn runtime configuration failed: %v", err)
		}
		scriptEngine, err = scriptevent.New(database, bridge)
		if err != nil {
			logger.Fatalf("script event engine configuration failed: %v", err)
		}
		scriptPreview, err = scriptevent.NewPreviewRunner(bridge)
		if err != nil {
			logger.Fatalf("script preview configuration failed: %v", err)
		}
		logger.Printf("Yarn authoring compiler: %s", scriptcontent.YarnCompilerVersion)
	} else {
		logger.Printf("Yarn authoring is disabled: YARN_COMPILER_PATH is not configured")
	}
	authManager := auth.NewManager(
		database,
		boolEnv("COOKIE_SECURE", true),
	)
	clock, err := newWorldClock()
	if err != nil {
		logger.Fatalf("invalid world clock configuration: %v", err)
	}
	if err := clock.SetWeather(stringEnv("WORLD_WEATHER", "clear")); err != nil {
		logger.Fatalf("invalid world weather configuration: %v", err)
	}
	world := worldstate.NewManager(clock)
	npcManifest, err := npcdata.Load()
	if err != nil {
		logger.Fatalf("NPC manifest startup failed: %v", err)
	}
	npcEngine, err := npc.NewEngine(npcManifest)
	if err != nil {
		logger.Fatalf("NPC simulation startup failed: %v", err)
	}
	restoreCtx, restoreCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	if err := npcEngine.Restore(restoreCtx, database); err != nil {
		restoreCancel()
		logger.Fatalf("NPC checkpoint restore failed: %v", err)
	}
	restoreCancel()
	initialWorld := world.Snapshot()
	if _, err := npcEngine.Tick(npc.TickTime{
		ServerTime: time.UnixMilli(initialWorld.ServerTimeMs),
		GameTime:   time.UnixMilli(initialWorld.GameTimeMs),
		DayNumber:  initialWorld.DayNumber,
		DayLength:  time.Duration(initialWorld.DayLengthMs) * time.Millisecond,
	}, nil); err != nil {
		logger.Fatalf("initial NPC simulation failed: %v", err)
	}
	activityPath := stringEnv(
		"ACTIVITY_LOG_PATH",
		"activity/events.jsonl",
	)
	activity, err := activitylog.Open(activityPath)
	if err != nil {
		logger.Fatalf("activity log could not be opened: %v", err)
	}
	defer activity.Close()
	logger.Printf("activity log: %s", activityPath)
	hub := realtime.NewHub(
		intEnv("MAX_CONNECTIONS", 100),
		world,
		logger,
		activity,
	)
	hub.SetLocationSaver(database)
	hub.SetChatMessageSaver(database)
	hub.SetNPCEngine(npcEngine)
	discordBridge, err := discordchat.NewFromEnvironment(hub.BroadcastExternalChat)
	if err != nil {
		logger.Fatalf("Discord chat configuration failed: %v", err)
	}
	if discordBridge != nil {
		discordBridge.Start()
		defer discordBridge.Close()
		hub.SetPublicChatSink(func(senderName, text string) {
			discordBridge.Enqueue(discordchat.Message{SenderName: senderName, Text: text})
		})
		logger.Printf("Discord game-chat bridge enabled")
	}
	if scriptEngine != nil {
		hub.SetScriptEngine(scriptEngine, npcManifest.AreaWorlds)
		logger.Printf("server-authoritative Yarn events enabled")
	}

	mux := http.NewServeMux()
	if discordBridge != nil {
		mux.Handle("/api/discord/game-chat", discordBridge.Handler())
	}
	mux.Handle("/ws", realtime.NewAuthenticatedWebSocketHandler(
		hub,
		originsEnv(),
		authManager,
		database,
	))
	mux.Handle("/api/world-state", httpapi.NewWorldStateHandler(
		world,
		func(state protocol.WorldState) {
			hub.BroadcastWorldState(state)
			hub.ResetNPCSchedules()
		},
	))
	mux.Handle("/api/status", httpapi.NewStatusHandler())
	adminHandler := httpapi.NewAdminHandler(
		stringEnv("ADMIN_KEY", ""), hub, database, startedAt, logLines,
	)
	mux.HandleFunc("/api/admin/stats", adminHandler.Stats)
	mux.HandleFunc("/api/admin/growth", adminHandler.Growth)
	mux.HandleFunc("/api/admin/chats", adminHandler.Chats)
	mux.HandleFunc("/api/admin/logs", adminHandler.Logs)
	mux.Handle(
		"/api/arcade-scores",
		httpapi.NewArcadeScoreHandler(authManager, database, hub),
	)
	scriptHandler := httpapi.NewScriptHandler(authManager, database)
	if scriptPreview != nil {
		scriptHandler = httpapi.NewScriptHandler(
			authManager, database, scriptPreview,
		)
	}
	mux.Handle("/api/scripts", scriptHandler)
	mux.Handle("/api/scripts/", scriptHandler)
	mux.Handle("/api/script-schema", httpapi.NewScriptSchemaHandler(database))
	accounts := httpapi.NewAccountHandler(authManager, database, hub)
	mux.HandleFunc("/api/auth/guest", accounts.Guest)
	mux.HandleFunc("/api/auth/login", accounts.Login)
	mux.HandleFunc("/api/auth/register", accounts.Register)
	mux.HandleFunc("/api/auth/logout", accounts.Logout)
	mux.HandleFunc("/api/session", accounts.Session)
	mux.HandleFunc("/api/characters", accounts.Characters)
	mux.HandleFunc("/api/characters/", accounts.Character)
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		healthCtx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ping(healthCtx); err != nil {
			http.Error(response, `{"ok":false,"database":false}`, http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true,"database":true}`))
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go world.Run(ctx, hub.BroadcastWorldState)
	go hub.RunPersistence(ctx)
	go hub.RunNPCSimulation(ctx)
	go npc.RunCheckpointPersistence(ctx, npcEngine, database, logger)

	server := &http.Server{
		Addr:              stringEnv("HTTP_ADDR", ":8080"),
		Handler:           corsMiddleware(mux, originsEnv()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Printf(
		"listening on %s (day=%s season=%s weather=%s maxConnections=%d)",
		server.Addr,
		worldstate.ShenmueDayLength,
		stringEnv("WORLD_SEASON", "summer"),
		stringEnv("WORLD_WEATHER", "clear"),
		intEnv("MAX_CONNECTIONS", 100),
	)
	serveErr := server.ListenAndServe()
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 8*time.Second)
	hub.FlushLocations(flushCtx)
	flushCancel()
	npcFlushCtx, npcFlushCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	if err := npc.FlushCheckpoints(npcFlushCtx, npcEngine, database); err != nil {
		logger.Printf("final NPC checkpoint save failed: %v", err)
	}
	npcFlushCancel()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Fatalf("server failed: %v", serveErr)
	}
}

func stringEnv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func intEnv(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func boolEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func newWorldClock() (*worldstate.Clock, error) {
	return worldstate.NewClock(
		worldEpoch(),
		stringEnv("WORLD_SEASON", "summer"),
	)
}

func worldEpoch() time.Time {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("WORLD_EPOCH_UNIX_MS")), 10, 64)
	if err != nil || value <= 0 {
		return time.Now()
	}
	return time.UnixMilli(value)
}

func originsEnv() []string {
	raw := stringEnv(
		"ALLOWED_ORIGINS",
		"http://localhost:5173,http://127.0.0.1:5173,https://www.newyokosuka.com",
	)
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimRight(strings.TrimSpace(part), "/"); value != "" {
			origins = append(origins, value)
		}
	}
	return origins
}

func corsMiddleware(next http.Handler, origins []string) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(request.Header.Get("Origin"), "/")
		if _, ok := allowed[origin]; ok {
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Vary", "Origin")
			response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Admin-Token")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, PATCH, OPTIONS")
			response.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}
