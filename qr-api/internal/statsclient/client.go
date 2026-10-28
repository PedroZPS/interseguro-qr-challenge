// Package statsclient llama por HTTP a la API Node que calcula las estadísticas.
package statsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// defaultTimeout acota la espera a Node cuando no se inyecta un cliente propio.
const defaultTimeout = 5 * time.Second

// Errores que el handler traduce a 503 y 502.
var (
	// ErrUnavailable: no se pudo hablar con Node (caído, red o timeout).
	ErrUnavailable = errors.New("el servicio de estadísticas no está disponible")
	// ErrBadResponse: Node respondió, pero con un error o algo ilegible.
	ErrBadResponse = errors.New("respuesta inválida del servicio de estadísticas")
)

// Stats es la respuesta de la API Node.
type Stats struct {
	Max              float64 `json:"max"`
	Min              float64 `json:"min"`
	Sum              float64 `json:"sum"`
	Average          float64 `json:"average"`
	DiagonalByMatrix struct {
		Q bool `json:"q"`
		R bool `json:"r"`
	} `json:"diagonalByMatrix"`
	AnyDiagonal bool `json:"anyDiagonal"`
}

// Client habla con la API Node en baseURL.
type Client struct {
	baseURL string
	http    *http.Client
}

// New crea un cliente. Si httpClient es nil se usa uno con timeout de 5 s.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: baseURL, http: httpClient}
}

// Stats envía Q y R a POST /stats y devuelve las estadísticas calculadas.
// token es el JWT de la petición original: Node también lo valida.
func (c *Client) Stats(ctx context.Context, token string, q, r [][]float64) (Stats, error) {
	var s Stats
	body, err := json.Marshal(map[string][][]float64{"q": q, "r": r})
	if err != nil {
		return s, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/stats", bytes.NewReader(body))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.http.Do(req)
	if err != nil {
		return s, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()

	// Go ya validó la matriz: cualquier no-200 de Node (incluso un 400) es un fallo del servicio.
	if res.StatusCode != http.StatusOK {
		return s, fmt.Errorf("%w: Node respondió %d", ErrBadResponse, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return Stats{}, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	return s, nil
}
