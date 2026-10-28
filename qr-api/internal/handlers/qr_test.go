package handlers

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/statsclient"
	"github.com/gofiber/fiber/v2"
)

const nodeOK = `{"max":5,"min":-0.8,"sum":13.2,"average":1.65,
"diagonalByMatrix":{"q":false,"r":false},"anyDiagonal":false}`

// fakeNode levanta un Node falso y cuenta cuántas veces lo llaman.
func fakeNode(t *testing.T, status int, body string) (url string, calls *atomic.Int32, lastBody *atomic.Value) {
	t.Helper()
	calls, lastBody = new(atomic.Int32), new(atomic.Value)
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		lastBody.Store(string(b))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(node.Close)
	return node.URL, calls, lastBody
}

func newApp(nodeURL string) *fiber.App {
	app := fiber.New()
	app.Post("/api/qr", QR(statsclient.New(nodeURL, nil)))
	return app
}

func post(t *testing.T, app *fiber.App, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/qr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestQR_Success(t *testing.T) {
	url, calls, lastBody := fakeNode(t, 200, nodeOK)

	status, out := post(t, newApp(url), `{"matrix":[[3,0],[4,5]]}`)

	if status != 200 {
		t.Fatalf("status = %d, quiero 200 (%v)", status, out)
	}
	q := out["q"].([]any)
	if math.Abs(q[0].([]any)[0].(float64)-0.6) > 1e-9 {
		t.Errorf("Q[0][0] = %v, quiero 0.6", q[0])
	}
	r := out["r"].([]any)
	if math.Abs(r[0].([]any)[0].(float64)-5) > 1e-9 {
		t.Errorf("R[0][0] = %v, quiero 5", r[0])
	}
	stats := out["stats"].(map[string]any)
	if stats["max"] != 5.0 || stats["anyDiagonal"] != false {
		t.Errorf("stats = %v", stats)
	}
	if calls.Load() != 1 || !strings.Contains(lastBody.Load().(string), `"r":[[5`) {
		t.Errorf("Node llamado %d veces con %v", calls.Load(), lastBody.Load())
	}
}

func TestQR_InvalidInputIs400AndNodeIsNotCalled(t *testing.T) {
	cases := map[string]string{
		"JSON roto":             `{"matrix": [[1,`,
		"sin campo matrix":      `{}`,
		"matriz vacía":          `{"matrix":[]}`,
		"filas irregulares":     `{"matrix":[[1,2],[3]]}`,
		"no numérico":           `{"matrix":[[1,"a"]]}`,
		"número fuera de rango": `{"matrix":[[1e999]]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			url, calls, _ := fakeNode(t, 200, nodeOK)

			status, out := post(t, newApp(url), body)

			if status != 400 {
				t.Errorf("status = %d, quiero 400", status)
			}
			if msg, _ := out["error"].(string); msg == "" {
				t.Errorf("falta mensaje de error: %v", out)
			}
			if calls.Load() != 0 {
				t.Errorf("Node no debería recibir llamadas con entrada inválida")
			}
		})
	}
}

func TestQR_NodeDownIs503(t *testing.T) {
	app := newApp("http://127.0.0.1:1") // puerto cerrado

	status, out := post(t, app, `{"matrix":[[3,0],[4,5]]}`)

	if status != 503 {
		t.Errorf("status = %d, quiero 503", status)
	}
	assertNoInternals(t, out)
}

func TestQR_NodeFailsIs502(t *testing.T) {
	url, _, _ := fakeNode(t, 500, `{"error":"boom"}`)

	status, out := post(t, newApp(url), `{"matrix":[[3,0],[4,5]]}`)

	if status != 502 {
		t.Errorf("status = %d, quiero 502", status)
	}
	assertNoInternals(t, out)
}

// El mensaje al cliente no debe filtrar direcciones ni detalles de red.
func assertNoInternals(t *testing.T, out map[string]any) {
	t.Helper()
	msg, _ := out["error"].(string)
	if msg == "" {
		t.Fatalf("falta mensaje de error: %v", out)
	}
	for _, leak := range []string{"127.0.0.1", "dial", "refused", "boom"} {
		if strings.Contains(msg, leak) {
			t.Errorf("el mensaje filtra %q: %s", leak, msg)
		}
	}
}

func TestQR_ForwardsCookieTokenToNode(t *testing.T) {
	calls := new(atomic.Int32)
	lastAuth := new(atomic.Value)
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		lastAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(nodeOK))
	}))
	t.Cleanup(node.Close)

	app := newApp(node.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/qr", strings.NewReader(`{"matrix":[[3,0],[4,5]]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "qr_session=token-de-prueba")

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status = %d, quiero 200", res.StatusCode)
	}
	if calls.Load() != 1 {
		t.Fatalf("Node llamado %d veces, quiero 1", calls.Load())
	}
	if got := lastAuth.Load(); got != "Bearer token-de-prueba" {
		t.Errorf("Node recibió Authorization = %v, quiero \"Bearer token-de-prueba\"", got)
	}
}

func TestQR_UnsupportedMediaType(t *testing.T) {
	app := newApp("http://127.0.0.1:1")
	req := httptest.NewRequest(http.MethodPost, "/api/qr", strings.NewReader(`matrix=[[1]]`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 415 {
		t.Errorf("status = %d, quiero 415", res.StatusCode)
	}
}
