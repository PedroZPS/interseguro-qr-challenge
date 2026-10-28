// Command server arranca la API Go: recibe matrices, calcula su QR y pide
// las estadísticas a la API Node.
package main

import (
	"log"
	"os"
	"time"

	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/auth"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/server"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/statsclient"
)

// env devuelve la variable de entorno key o def si no está definida.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// mustEnv devuelve la variable key o termina el proceso si falta: es mejor
// fallar al arrancar que correr sin autenticación.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("falta la variable de entorno %s", key)
	}
	return v
}

func main() {
	key, err := auth.ParsePrivateKey(mustEnv("JWT_PRIVATE_KEY"))
	if err != nil {
		log.Fatalf("JWT_PRIVATE_KEY inválida: %v", err)
	}
	ttl, err := time.ParseDuration(env("JWT_TTL", "1h"))
	if err != nil {
		log.Fatalf("JWT_TTL inválida: %v", err)
	}

	app := server.New(server.Config{
		Key:      key,
		TTL:      ttl,
		User:     mustEnv("DEMO_USER"),
		Password: mustEnv("DEMO_PASSWORD"),
		Stats:    statsclient.New(env("STATS_API_URL", "http://localhost:3000"), nil),
		// Por defecto, la carpeta frontend/ del repo (arranque con `go run` desde qr-api/).
		FrontendDir: env("FRONTEND_DIR", "../frontend"),
	})

	log.Fatal(app.Listen(":" + env("PORT", "8080")))
}
