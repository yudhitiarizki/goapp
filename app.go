package goapp

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yudhitiarizki/goapp/logging"
	"github.com/yudhitiarizki/goapp/reqctx"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Config holds the few knobs the application needs at startup.
type Config struct {
	DB            DatabaseConfig // postgres connection, DSN is built from its fields
	Port          string         // listen address, e.g. ":8080"
	Prefix        string         // route prefix, defaults to "/api/v1"
	AutoMigrate   bool           // run AutoMigrate for all Migrate() models on boot
	JWTSecret     string         // HMAC secret for parsing/signing Bearer JWTs (empty disables auth)
	JWTAccessTTL  time.Duration  // access token lifetime (0 = 15m)
	JWTRefreshTTL time.Duration  // refresh token lifetime (0 = 7 days)
	LogFile       string         // JSON log destination file (empty = stdout)
	LogEnabled    bool           // master switch for all logging (false = off)
	LogDisable    []string       // log types to silence, e.g. ["db","adaptor"]
}

// Run boots the application: connects the database, wires every registered
// module, runs migrations, registers routes, and starts the HTTP server with
// graceful shutdown. It blocks until SIGINT/SIGTERM.
func Run(cfg Config) {
	fx.New(
		fx.Supply(cfg),
		fx.Provide(newDB, newEngine, newSigner, newHTTPClient, newLogger),
		fx.Options(modules...),
		fx.Invoke(autoMigrate, registerRoutes, startServer),
	).Run()
}

func newSigner(cfg Config) *reqctx.Signer {
	return reqctx.NewSigner(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
}

// newHTTPClient provides the shared outbound HTTP client. Its transport
// forwards the request id on every call, so any adapter injecting *http.Client
// gets distributed tracing for free. A 15s timeout guards against hung peers.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: reqctx.Transport{Base: http.DefaultTransport},
	}
}

func newLogger(cfg Config) *logging.Logger {
	return logging.New(logging.FileWriter(cfg.LogFile), cfg.LogEnabled, cfg.LogDisable)
}

func newEngine(cfg Config, logger *logging.Logger) *gin.Engine {
	e := gin.New()
	// Logger + recovery first so they wrap the whole chain and log after the
	// request id is set; then reqctx builds the ApiHeader and JWT auth fills in
	// the caller id.
	e.Use(logger.GinLogger(), logger.GinRecovery())
	e.Use(reqctx.Middleware(), reqctx.Auth(cfg.JWTSecret))
	return e
}

func newDB(cfg Config, logger *logging.Logger) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(cfg.DB.DSN()), &gorm.Config{
		Logger: logger.GormLogger(),
	})
}

type migrateParams struct {
	fx.In
	Config Config
	DB     *gorm.DB
	Models []any `group:"models"`
}

func autoMigrate(p migrateParams) error {
	if !p.Config.AutoMigrate || len(p.Models) == 0 {
		return nil
	}
	return p.DB.AutoMigrate(p.Models...)
}

type routeParams struct {
	fx.In
	Config Config
	Engine *gin.Engine
	Routes []RouteRegistrar `group:"routes"`
}

func registerRoutes(p routeParams) {
	prefix := p.Config.Prefix
	g := p.Engine.Group(prefix)
	for _, r := range p.Routes {
		r.RegisterRoutes(g)
	}
}

func startServer(lc fx.Lifecycle, cfg Config, e *gin.Engine) {
	srv := &http.Server{Addr: cfg.Port, Handler: e}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			go srv.Serve(ln)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
}
