package goapp

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Option is an alias for fx.Option so domains never import fx directly.
type Option = fx.Option

// RouteRegistrar is implemented by every handler. The library collects all
// registrars and calls RegisterRoutes on each one after wiring is done.
type RouteRegistrar interface {
	RegisterRoutes(r *gin.RouterGroup)
}

// modules holds every module declared via Module(). Package-level variable
// initialization runs sequentially in a single goroutine before main(), so no
// locking is needed here.
var modules []Option

// Module declares a domain. Call it once per domain, assigning the result to an
// exported package variable so the domain registers itself when imported:
//
//	var Module = goapp.Module("dokter",
//	    goapp.Provide(NewRepository, NewService),
//	    goapp.Handler(NewHandler),
//	    goapp.Migrate(&Dokter{}),
//	)
func Module(name string, opts ...Option) Option {
	m := fx.Module(name, opts...)
	modules = append(modules, m)
	return m
}

// Provide registers plain constructors (repositories, services, etc). Each
// constructor's parameters are resolved by type from everything else provided.
func Provide(constructors ...any) Option {
	return fx.Provide(constructors...)
}

// Handler registers constructors whose results are added to the "routes" group
// as RouteRegistrar. Any dependency the handler needs (including services from
// other domains) is matched by its parameter type.
func Handler(constructors ...any) Option {
	opts := make([]Option, len(constructors))
	for i, c := range constructors {
		opts[i] = fx.Provide(fx.Annotate(c,
			fx.As(new(RouteRegistrar)),
			fx.ResultTags(`group:"routes"`),
		))
	}
	return fx.Options(opts...)
}

// Invoke registers functions that fx runs at startup, after all providers are
// built. Use it to start background workers (pollers, consumers, schedulers).
//
// Do NOT start the worker in the invoke body itself — that runs during wiring,
// before autoMigrate. Instead take fx.Lifecycle and register an OnStart hook
// (spawn the goroutine) and an OnStop hook (cancel/stop it), so the worker
// starts after migrations and stops cleanly on graceful shutdown:
//
//	var Module = goapp.Module("poller",
//	    goapp.Provide(NewPoller),
//	    goapp.Invoke(func(lc fx.Lifecycle, p *Poller) {
//	        lc.Append(fx.Hook{
//	            OnStart: func(ctx context.Context) error { go p.Run(); return nil },
//	            OnStop:  func(ctx context.Context) error { return p.Stop() },
//	        })
//	    }),
//	)
func Invoke(funcs ...any) Option {
	return fx.Invoke(funcs...)
}

// Migrate registers GORM models for AutoMigrate. Migration runs at startup only
// when Config.AutoMigrate is true. Models are collected as `any` so different
// concrete types can share one group.
func Migrate(models ...any) Option {
	opts := make([]Option, len(models))
	for i := range models {
		m := models[i]
		opts[i] = fx.Provide(fx.Annotate(
			func() any { return m },
			fx.ResultTags(`group:"models"`),
		))
	}
	return fx.Options(opts...)
}
