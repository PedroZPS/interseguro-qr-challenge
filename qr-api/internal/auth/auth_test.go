package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

const (
	demoUser = "reviewer"
	demoPass = "s3cret-demo"
)

var (
	keyOnce  sync.Once
	goodKey  *rsa.PrivateKey
	otherKey *rsa.PrivateKey
)

// keys genera (una sola vez) el par legítimo y uno ajeno, porque RSA-2048 es lento.
func keys(t *testing.T) (good, other *rsa.PrivateKey) {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if goodKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if otherKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return goodKey, otherKey
}

func newApp(t *testing.T) *fiber.App {
	t.Helper()
	key, _ := keys(t)
	app := fiber.New()
	app.Post("/auth/login", Login(Config{Key: key, TTL: time.Hour, User: demoUser, Password: demoPass}))
	app.Get("/protected", Middleware(&key.PublicKey), func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func call(t *testing.T, app *fiber.App, method, path, body, authHeader string) (int, map[string]any) {
	t.Helper()
	headers := map[string]string{}
	if authHeader != "" {
		headers["Authorization"] = authHeader
	}
	res, out := callWithHeaders(t, app, method, path, body, headers)
	return res.StatusCode, out
}

func callWithHeaders(t *testing.T, app *fiber.App, method, path, body string, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if _, ok := headers["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res, out
}

func TestLogin_SuccessGivesUsableToken(t *testing.T) {
	app := newApp(t)

	status, out := call(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"s3cret-demo"}`, "")

	if status != 200 {
		t.Fatalf("status = %d, quiero 200 (%v)", status, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatalf("falta token: %v", out)
	}
	if status, _ := call(t, app, http.MethodGet, "/protected", "", "Bearer "+token); status != 200 {
		t.Errorf("el token recién emitido da %d en /protected, quiero 200", status)
	}

	// Claims: sujeto y caducidad de ~1 h.
	key, _ := keys(t)
	claims := jwt.RegisteredClaims{}
	if _, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"})); err != nil {
		t.Fatalf("el token no verifica: %v", err)
	}
	if claims.Subject != demoUser {
		t.Errorf("sub = %q, quiero %q", claims.Subject, demoUser)
	}
	if ttl := time.Until(claims.ExpiresAt.Time); ttl < 59*time.Minute || ttl > time.Hour {
		t.Errorf("caduca en %v, quiero ~1 h", ttl)
	}
}

func TestLogin_Rejects(t *testing.T) {
	app := newApp(t)
	cases := map[string]struct {
		body string
		want int
	}{
		"contraseña incorrecta": {`{"username":"reviewer","password":"mal"}`, 401},
		"usuario incorrecto":    {`{"username":"otro","password":"s3cret-demo"}`, 401},
		"credenciales vacías":   {`{}`, 401},
		"JSON roto":             {`{"username":`, 400},
	}
	messages := map[string]string{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, out := call(t, app, http.MethodPost, "/auth/login", tc.body, "")
			if status != tc.want {
				t.Errorf("status = %d, quiero %d", status, tc.want)
			}
			if _, has := out["token"]; has {
				t.Errorf("no debe devolver token")
			}
			messages[name], _ = out["error"].(string)
		})
	}
	// No se debe revelar si falló el usuario o la contraseña.
	if messages["contraseña incorrecta"] != messages["usuario incorrecto"] {
		t.Errorf("mensajes distintos filtran qué campo falló: %q vs %q",
			messages["contraseña incorrecta"], messages["usuario incorrecto"])
	}
}

func TestMiddleware_Rejects(t *testing.T) {
	app := newApp(t)
	good, other := keys(t)

	sign := func(m jwt.SigningMethod, claims jwt.Claims, key any) string {
		s, err := jwt.NewWithClaims(m, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	valid := jwt.RegisteredClaims{Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	expired := jwt.RegisteredClaims{Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}
	noExp := jwt.RegisteredClaims{Subject: "x"}

	// Ataque de confusión de algoritmo: HS256 usando la clave PÚBLICA (PEM) como secreto.
	pubDER, _ := x509.MarshalPKIXPublicKey(&good.PublicKey)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	cases := map[string]string{
		"sin cabecera":            "",
		"esquema Basic":           "Basic " + sign(jwt.SigningMethodRS256, valid, good),
		"token basura":            "Bearer no.es.un-jwt",
		"firmado con otra clave":  "Bearer " + sign(jwt.SigningMethodRS256, valid, other),
		"caducado":                "Bearer " + sign(jwt.SigningMethodRS256, expired, good),
		"legítimo sin expiración": "Bearer " + sign(jwt.SigningMethodRS256, noExp, good),
		"alg none":                "Bearer " + sign(jwt.SigningMethodNone, valid, jwt.UnsafeAllowNoneSignatureType),
		"HS256 con clave pública": "Bearer " + sign(jwt.SigningMethodHS256, valid, pubPEM),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			status, out := call(t, app, http.MethodGet, "/protected", "", header)
			if status != 401 {
				t.Errorf("status = %d, quiero 401", status)
			}
			msg, _ := out["error"].(string)
			if msg == "" || strings.Contains(strings.ToLower(msg), "signature") || strings.Contains(strings.ToLower(msg), "jwt") {
				t.Errorf("mensaje de error ausente o filtra detalles: %q", msg)
			}
		})
	}
}

func TestParsePrivateKey(t *testing.T) {
	key, _ := keys(t)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(key)
	pkcs1 := x509.MarshalPKCS1PrivateKey(key)
	enc := func(typ string, der []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	}

	for name, in := range map[string]string{
		"PKCS#8": enc("PRIVATE KEY", pkcs8),
		"PKCS#1": enc("RSA PRIVATE KEY", pkcs1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePrivateKey(in)
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			if got.N.Cmp(key.N) != 0 {
				t.Errorf("la clave leída no coincide")
			}
		})
	}
	for name, in := range map[string]string{
		"vacía":         "",
		"no es base64":  "%%%no%%%",
		"no es PEM":     base64.StdEncoding.EncodeToString([]byte("hola")),
		"PEM sin clave": enc("PUBLIC KEY", []byte("xx")),
	} {
		t.Run("rechaza "+name, func(t *testing.T) {
			if _, err := ParsePrivateKey(in); err == nil {
				t.Errorf("debería fallar")
			}
		})
	}
}

func TestLogin_SetsHttpOnlyCookie(t *testing.T) {
	app := newApp(t)

	res, out := callWithHeaders(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"s3cret-demo"}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, quiero 200 (%v)", res.StatusCode, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatalf("falta token en JSON")
	}

	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == CookieName {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("no se emitió la cookie %s en Set-Cookie", CookieName)
	}
	if cookie.Value != token {
		t.Errorf("cookie value = %q, quiero %q", cookie.Value, token)
	}
	if !cookie.HttpOnly {
		t.Errorf("HttpOnly = false, quiero true")
	}
	if !cookie.Secure {
		t.Errorf("Secure = false, quiero true")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, quiero Strict (%v)", cookie.SameSite, http.SameSiteStrictMode)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, quiero \"/\"", cookie.Path)
	}
	if cookie.MaxAge != 3600 {
		t.Errorf("MaxAge = %d, quiero 3600", cookie.MaxAge)
	}
}

func TestLogin_FailedLoginDoesNotSetCookie(t *testing.T) {
	app := newApp(t)

	res, _ := callWithHeaders(t, app, http.MethodPost, "/auth/login", `{"username":"reviewer","password":"mal"}`, nil)
	if res.StatusCode != 401 {
		t.Fatalf("status = %d, quiero 401", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == CookieName {
			t.Errorf("un login fallido no debe poner la cookie %s", CookieName)
		}
	}
}

func TestMiddleware_CookieAuth(t *testing.T) {
	app := newApp(t)
	good, other := keys(t)

	sign := func(claims jwt.Claims, key any) string {
		s, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	valid := jwt.RegisteredClaims{Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	expired := jwt.RegisteredClaims{Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}
	validToken := sign(valid, good)

	// Cookie válida → 200
	res, _ := callWithHeaders(t, app, http.MethodGet, "/protected", "", map[string]string{
		"Cookie": CookieName + "=" + validToken,
	})
	if res.StatusCode != 200 {
		t.Errorf("cookie válida dio %d, quiero 200", res.StatusCode)
	}

	// Cookies inválidas → 401 con mensaje uniforme
	badCookies := map[string]string{
		"cookie basura":          "no-es-un-jwt",
		"cookie caducada":        sign(expired, good),
		"firmada con otra clave": sign(valid, other),
	}
	for name, val := range badCookies {
		t.Run(name, func(t *testing.T) {
			res, out := callWithHeaders(t, app, http.MethodGet, "/protected", "", map[string]string{
				"Cookie": CookieName + "=" + val,
			})
			if res.StatusCode != 401 {
				t.Errorf("%s dio status = %d, quiero 401", name, res.StatusCode)
			}
			msg, _ := out["error"].(string)
			if msg != "token inválido o ausente" {
				t.Errorf("mensaje = %q, quiero \"token inválido o ausente\"", msg)
			}
		})
	}

	// Precedencia: Bearer manda sobre Cookie
	t.Run("Bearer tiene precedencia sobre cookie", func(t *testing.T) {
		res, _ := callWithHeaders(t, app, http.MethodGet, "/protected", "", map[string]string{
			"Authorization": "Bearer " + validToken,
			"Cookie":        CookieName + "=cookie-caducada-o-invalida",
		})
		if res.StatusCode != 200 {
			t.Errorf("Bearer legítimo con cookie basura debe dar 200, dio %d", res.StatusCode)
		}
	})
}

func TestMiddleware_CSRFProtection(t *testing.T) {
	app := newApp(t)
	good, _ := keys(t)

	validToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Subject:   "x",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(good)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		headers    map[string]string
		wantStatus int
		wantErr    string
	}{
		{
			name: "cookie con Sec-Fetch-Site cross-site es 403",
			headers: map[string]string{
				"Cookie":         CookieName + "=" + validToken,
				"Sec-Fetch-Site": "cross-site",
			},
			wantStatus: 403,
			wantErr:    "origen no permitido",
		},
		{
			name: "cookie con Sec-Fetch-Site same-origin es 200",
			headers: map[string]string{
				"Cookie":         CookieName + "=" + validToken,
				"Sec-Fetch-Site": "same-origin",
			},
			wantStatus: 200,
		},
		{
			name: "cookie sin Sec-Fetch-Site es 200",
			headers: map[string]string{
				"Cookie": CookieName + "=" + validToken,
			},
			wantStatus: 200,
		},
		{
			name: "Bearer con Sec-Fetch-Site cross-site es 200 (CSRF solo aplica a cookies)",
			headers: map[string]string{
				"Authorization":  "Bearer " + validToken,
				"Sec-Fetch-Site": "cross-site",
			},
			wantStatus: 200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, out := callWithHeaders(t, app, http.MethodGet, "/protected", "", tc.headers)
			if res.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, quiero %d (%v)", res.StatusCode, tc.wantStatus, out)
			}
			if tc.wantErr != "" {
				msg, _ := out["error"].(string)
				if msg != tc.wantErr {
					t.Errorf("error = %q, quiero %q", msg, tc.wantErr)
				}
			}
		})
	}
}
