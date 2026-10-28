// Package handlers contiene los handlers HTTP de la API Go.
package handlers

import (
	"errors"
	"log"

	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/auth"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/qr"
	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/statsclient"
	"github.com/gofiber/fiber/v2"
)

type qrRequest struct {
	Matrix [][]float64 `json:"matrix"`
}

type qrResponse struct {
	Q     [][]float64       `json:"q"`
	R     [][]float64       `json:"r"`
	Stats statsclient.Stats `json:"stats"`
}

// QR devuelve el handler de POST /api/qr: factoriza la matriz, pide las
// estadísticas de Q y R a la API Node y responde con todo junto.
func QR(stats *statsclient.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Capa CSRF: exige application/json. Evita que un formulario simple de otro origen dispare la acción.
		if !c.Is("json") {
			return fail(c, fiber.StatusUnsupportedMediaType, "se espera Content-Type: application/json")
		}

		var req qrRequest
		if err := c.BodyParser(&req); err != nil {
			return fail(c, fiber.StatusBadRequest, "JSON inválido: se espera {\"matrix\": [[...]]}")
		}

		q, r, err := qr.Decompose(req.Matrix)
		if err != nil {
			// Los errores de qr son mensajes pensados para el cliente.
			return fail(c, fiber.StatusBadRequest, err.Error())
		}

		// El token ya lo validó el middleware; se reenvía para que Node lo valide también.
		token, _ := auth.Token(c)
		s, err := stats.Stats(c.UserContext(), token, q, r)
		switch {
		case errors.Is(err, statsclient.ErrUnavailable):
			log.Printf("stats-api no disponible: %v", err)
			return fail(c, fiber.StatusServiceUnavailable, "el servicio de estadísticas no está disponible")
		case errors.Is(err, statsclient.ErrBadResponse):
			log.Printf("stats-api respuesta inválida: %v", err)
			return fail(c, fiber.StatusBadGateway, "el servicio de estadísticas respondió con un error")
		case err != nil:
			log.Printf("error inesperado: %v", err)
			return fail(c, fiber.StatusInternalServerError, "error interno")
		}

		return c.JSON(qrResponse{Q: q, R: r, Stats: s})
	}
}

// fail responde con {"error": msg}. Nunca incluye stack traces ni detalles internos.
func fail(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}
