package server

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PedroZPS/interseguro-qr-challenge/qr-api/internal/statsclient"
	"github.com/gofiber/fiber/v2"
)

const nodeOK = `{"max":5,"min":-0.8,"sum":13.2,"average":1.65,
"diagonalByMatrix":{"q":false,"r":false},"anyDiagonal":false}`

const matrixBody = `{"matrix":[[3,0],[4,5]]}`

var testKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

// fakeNode cuenta llamadas y guarda la cabecera Authorization recibida.
func fakeNode(t *testing.T) (client *statsclient.Client, calls *atomic.Int32, gotAuth *atomic.Value) {
	t.Helper()
	calls, gotAuth = new(atomic.Int32), new(atomic.Value)
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(nodeOK))
	}))
	t.Cleanup(node.Close)
	return statsclient.New(node.URL, nil), calls, gotAuth
}

func testApp(t *testing.T, mutate func(*Config)) (*fiber.App, *atomic.Int32, *atomic.Value) {
	t.Helper()
	client, calls, gotAuth := fakeNode(t)
	cfg := Config{Key: testKey, TTL: time.Hour, User: "reviewer", Password: "pw-demo", Stats: client}
	if mutate != nil {
		mutate(&cfg)
	}
	return New(cfg), calls, gotAuth
}

func do(t *testing.T, app *fiber.App, method, path, body string, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res, out
}

func login(t *testing.T, app *fiber.App) string {
	t.Helper()
	res, out := do(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"pw-demo"}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("login = %d (%v)", res.StatusCode, out)
	}
	return out["token"].(string)
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestFlow_LoginThenQR(t *testing.T) {
	app, calls, gotAuth := testApp(t, nil)
	token := login(t, app)

	res, out := do(t, app, http.MethodPost, "/api/qr", matrixBody, bearer(token))

	if res.StatusCode != 200 {
		t.Fatalf("status = %d, quiero 200 (%v)", res.StatusCode, out)
	}
	if out["stats"].(map[string]any)["max"] != 5.0 {
		t.Errorf("stats = %v", out["stats"])
	}
	if calls.Load() != 1 || gotAuth.Load() != "Bearer "+token {
		t.Errorf("Node: %d llamadas, Authorization = %v; quiero 1 y el mismo token", calls.Load(), gotAuth.Load())
	}
}

func TestQR_WithoutTokenIs401AndNodeIsNotCalled(t *testing.T) {
	app, calls, _ := testApp(t, nil)

	res, out := do(t, app, http.MethodPost, "/api/qr", matrixBody, nil)

	if res.StatusCode != 401 || out["error"] == nil {
		t.Errorf("status = %d (%v), quiero 401 con error", res.StatusCode, out)
	}
	if calls.Load() != 0 {
		t.Errorf("Node no debe recibir llamadas sin token")
	}
}

func TestHealthIsPublic(t *testing.T) {
	app, _, _ := testApp(t, nil)

	res, _ := do(t, app, http.MethodGet, "/health", "", nil)

	if res.StatusCode != 200 {
		t.Errorf("status = %d, quiero 200", res.StatusCode)
	}
}

func TestLogin_RateLimited(t *testing.T) {
	app, _, _ := testApp(t, func(c *Config) { c.LoginRateLimit = 3 })
	bad := `{"username":"reviewer","password":"mal"}`

	for i := 1; i <= 3; i++ {
		if res, _ := do(t, app, http.MethodPost, "/auth/login", bad, nil); res.StatusCode != 401 {
			t.Fatalf("intento %d: status = %d, quiero 401", i, res.StatusCode)
		}
	}
	res, out := do(t, app, http.MethodPost, "/auth/login", bad, nil)

	if res.StatusCode != 429 || out["error"] == nil {
		t.Errorf("status = %d (%v), quiero 429 con error", res.StatusCode, out)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Errorf("falta Retry-After")
	}
}

func TestAPI_RateLimited(t *testing.T) {
	app, _, _ := testApp(t, func(c *Config) { c.RateLimit = 2 })
	token := login(t, app)

	for i := 1; i <= 2; i++ {
		if res, _ := do(t, app, http.MethodPost, "/api/qr", matrixBody, bearer(token)); res.StatusCode != 200 {
			t.Fatalf("petición %d: status = %d, quiero 200", i, res.StatusCode)
		}
	}
	res, _ := do(t, app, http.MethodPost, "/api/qr", matrixBody, bearer(token))

	if res.StatusCode != 429 {
		t.Errorf("status = %d, quiero 429", res.StatusCode)
	}
}

// app.Test devuelve el error de fasthttp en vez de la respuesta, así que este test
// usa un socket real y declara un Content-Length mayor que el límite (1 MB).
func TestBodyTooLargeIs413JSON(t *testing.T) {
	app, _, _ := testApp(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("POST /api/qr HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 2000000\r\n\r\n"))

	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("sin respuesta: %v", err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)

	if res.StatusCode != 413 || out["error"] == nil {
		t.Errorf("status = %d (%v), quiero 413 con error JSON", res.StatusCode, out)
	}
}

func TestUnknownRouteIs404JSON(t *testing.T) {
	app, _, _ := testApp(t, nil)

	res, out := do(t, app, http.MethodGet, "/nada", "", nil)

	if res.StatusCode != 404 || out["error"] == nil {
		t.Errorf("status = %d (%v), quiero 404 con error JSON", res.StatusCode, out)
	}
}

func TestCORSIsClosed(t *testing.T) {
	app, _, _ := testApp(t, nil)

	res, _ := do(t, app, http.MethodGet, "/health", "", map[string]string{"Origin": "https://otro-sitio.example"})

	if h := res.Header.Get("Access-Control-Allow-Origin"); h != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, quiero ausente", h)
	}
}

// Un error inesperado o un pánico dan 500 genérico: nada de detalles internos.
func TestUnexpectedErrorsAreGeneric500(t *testing.T) {
	app := newFiber()
	app.Get("/err", func(c *fiber.Ctx) error { return errors.New("dial tcp 10.0.0.5:5432: password=hunter2") })
	app.Get("/panic", func(c *fiber.Ctx) error { panic("stack secreto") })

	for _, path := range []string{"/err", "/panic"} {
		t.Run(path, func(t *testing.T) {
			res, out := do(t, app, http.MethodGet, path, "", nil)

			if res.StatusCode != 500 {
				t.Errorf("status = %d, quiero 500", res.StatusCode)
			}
			msg, _ := out["error"].(string)
			if msg != "error interno" {
				t.Errorf("error = %q, quiero el mensaje genérico", msg)
			}
		})
	}
}

func TestFrontend_ServedWithSecurityHeaders(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"index.html": "<h1>Factorización QR</h1>",
		"app.js":     "console.log(1)",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app, _, _ := testApp(t, func(c *Config) { c.FrontendDir = dir })

	get := func(path string) (*http.Response, string) {
		res, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	res, body := get("/")
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "Factorización QR") {
		t.Errorf("GET / = %d %q, quiero la página HTML", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q, quiero default-src 'self'", csp)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("falta X-Content-Type-Options: nosniff")
	}
	if res, body := get("/app.js"); res.StatusCode != 200 || body != "console.log(1)" {
		t.Errorf("GET /app.js = %d %q", res.StatusCode, body)
	}
	// Las rutas de la API siguen mandando sobre los archivos estáticos.
	if res, _ := get("/health"); res.StatusCode != 200 {
		t.Errorf("GET /health = %d, quiero 200", res.StatusCode)
	}
	// Un archivo que no existe sigue dando el 404 en JSON, no uno en texto plano.
	if res, _ := get("/no-existe.css"); res.StatusCode != 404 || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		t.Errorf("GET /no-existe.css = %d %q, quiero 404 JSON", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestFlow_LoginWithCookieThenQR(t *testing.T) {
	app, calls, gotAuth := testApp(t, nil)

	res, out := do(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"pw-demo"}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("login = %d (%v)", res.StatusCode, out)
	}
	var sessionCookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "qr_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("falta cookie qr_session en la respuesta de login")
	}

	// Llamada a /api/qr solo con la cookie (sin cabecera Authorization)
	qrRes, qrOut := do(t, app, http.MethodPost, "/api/qr", matrixBody, map[string]string{
		"Cookie": "qr_session=" + sessionCookie.Value,
	})
	if qrRes.StatusCode != 200 {
		t.Fatalf("status = %d, quiero 200 (%v)", qrRes.StatusCode, qrOut)
	}
	if qrOut["stats"].(map[string]any)["max"] != 5.0 {
		t.Errorf("stats = %v", qrOut["stats"])
	}
	if calls.Load() != 1 || gotAuth.Load() != "Bearer "+sessionCookie.Value {
		t.Errorf("Node: %d llamadas, Authorization = %v; quiero 1 y Bearer con el token de la cookie", calls.Load(), gotAuth.Load())
	}
}

func TestAuthLogout(t *testing.T) {
	app, _, _ := testApp(t, nil)

	res, _ := do(t, app, http.MethodPost, "/auth/logout", "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("status = %d, quiero 204", res.StatusCode)
	}

	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "qr_session" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("falta cookie qr_session en Set-Cookie de logout")
	}
	if cookie.Value != "" {
		t.Errorf("valor de la cookie = %q, quiero vacío", cookie.Value)
	}
	if cookie.MaxAge > 0 {
		t.Errorf("MaxAge = %d, quiero <= 0", cookie.MaxAge)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Errorf("atributos incorrectos en cookie de logout: HttpOnly=%v, Secure=%v, SameSite=%v, Path=%q",
			cookie.HttpOnly, cookie.Secure, cookie.SameSite, cookie.Path)
	}
}

func TestAuthSession(t *testing.T) {
	app, _, _ := testApp(t, nil)

	// Sin token ni cookie → 401
	res, _ := do(t, app, http.MethodGet, "/auth/session", "", nil)
	if res.StatusCode != 401 {
		t.Errorf("GET /auth/session sin cookie dio %d, quiero 401", res.StatusCode)
	}

	// Login para obtener cookie
	loginRes, _ := do(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"pw-demo"}`, nil)
	var sessionCookie *http.Cookie
	for _, c := range loginRes.Cookies() {
		if c.Name == "qr_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("falta cookie tras login")
	}

	// Con cookie válida → 204
	sessRes, _ := do(t, app, http.MethodGet, "/auth/session", "", map[string]string{
		"Cookie": "qr_session=" + sessionCookie.Value,
	})
	if sessRes.StatusCode != 204 {
		t.Errorf("GET /auth/session con cookie válida dio %d, quiero 204", sessRes.StatusCode)
	}
}

func TestQR_UnsupportedMediaTypeIs415AndNodeNotCalled(t *testing.T) {
	app, calls, _ := testApp(t, nil)
	token := login(t, app)

	cases := []string{"application/x-www-form-urlencoded", "text/plain"}
	for _, ct := range cases {
		t.Run(ct, func(t *testing.T) {
			before := calls.Load()
			res, out := do(t, app, http.MethodPost, "/api/qr", matrixBody, map[string]string{
				"Content-Type":  ct,
				"Authorization": "Bearer " + token,
			})
			if res.StatusCode != 415 {
				t.Errorf("status = %d para Content-Type %s, quiero 415 (%v)", res.StatusCode, ct, out)
			}
			if msg, _ := out["error"].(string); msg != "se espera Content-Type: application/json" {
				t.Errorf("mensaje = %q, quiero \"se espera Content-Type: application/json\"", msg)
			}
			if calls.Load() != before {
				t.Errorf("Node no debe ser llamado si Content-Type es inválido")
			}
		})
	}
}
