// Package server arma la app Fiber completa: rutas, seguridad y manejo de errores.
package server

import (
	"crypto/rsa"
	"errors"
	"log"
	"time"

	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/auth"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/handlers"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/statsclient"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const (
	bodyLimit             = 1 << 20 // 1 MB: dos matrices de 100×100 caben de sobra
	defaultRateLimit      = 30      // peticiones por minuto y por IP en /api
	defaultLoginRateLimit = 5       // intentos de login por minuto y por IP (fuerza bruta)
)

// Config reúne lo que necesita la app. RateLimit y LoginRateLimit a 0 usan los valores por defecto.
type Config struct {
	Key            *rsa.PrivateKey
	TTL            time.Duration
	User, Password string
	Stats          *statsclient.Client
	RateLimit      int
	LoginRateLimit int
	// FrontendDir es la carpeta con la página (index.html, app.js, styles.css).
	// Vacía = no se sirve frontend.
	FrontendDir string
}

// newFiber crea la app con los límites, el recover y el manejador de errores en JSON.
func newFiber() *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:    bodyLimit,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorHandler: errorHandler,
		// Detrás del ingress de Azure la IP real llega en X-Forwarded-For.
		ProxyHeader: fiber.HeaderXForwardedFor,
	})
	// Un pánico en un handler se convierte en error (500 genérico) y no tumba el proceso.
	app.Use(recover.New())
	app.Use(securityHeaders)
	return app
}

// New devuelve la app lista para escuchar. No habilita CORS a propósito:
// el frontend lo sirve esta misma API, así que no hay peticiones entre orígenes.
func New(cfg Config) *fiber.App {
	app := newFiber()

	app.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })

	app.Post("/auth/login", rateLimit(orDefault(cfg.LoginRateLimit, defaultLoginRateLimit)),
		auth.Login(auth.Config{Key: cfg.Key, TTL: cfg.TTL, User: cfg.User, Password: cfg.Password}))
	app.Post("/auth/logout", auth.Logout)
	app.Get("/auth/session", auth.Middleware(&cfg.Key.PublicKey), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	api := app.Group("/api",
		rateLimit(orDefault(cfg.RateLimit, defaultRateLimit)),
		auth.Middleware(&cfg.Key.PublicKey),
	)
	api.Post("/qr", handlers.QR(cfg.Stats))

	// Va al final: las rutas de la API tienen prioridad y un archivo que no existe
	// cae al 404 en JSON del manejador de errores.
	if cfg.FrontendDir != "" {
		app.Static("/", cfg.FrontendDir)
	}

	return app
}

// securityHeaders: la página no usa scripts ni estilos inline, así que la CSP
// puede ser estricta (solo recursos del mismo origen) y no permitir iframes.
func securityHeaders(c *fiber.Ctx) error {
	c.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Referrer-Policy", "no-referrer")
	return c.Next()
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// rateLimit limita a max peticiones por minuto y por IP; al pasarse responde 429 en JSON.
func rateLimit(max int) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "demasiadas peticiones, intenta más tarde"})
		},
	})
}

// errorHandler responde siempre en JSON y nunca expone detalles internos.
func errorHandler(c *fiber.Ctx, err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		msg := fe.Message
		switch fe.Code {
		case fiber.StatusNotFound:
			msg = "ruta no encontrada"
		case fiber.StatusRequestEntityTooLarge:
			msg = "body demasiado grande"
		}
		return c.Status(fe.Code).JSON(fiber.Map{"error": msg})
	}
	log.Printf("error interno: %v", err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "error interno"})
}
