// Package auth emite y valida JWT RS256: Go firma con la clave privada y
// cualquier servicio que solo tenga la pública (Node) puede verificarlos.
package auth

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// CookieName es el nombre de la cookie de sesión HttpOnly.
const CookieName = "qr_session"

// Config de Login: clave de firma, duración del token y el usuario demo.
type Config struct {
	Key      *rsa.PrivateKey
	TTL      time.Duration
	User     string
	Password string
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login devuelve el handler de POST /auth/login. Si las credenciales coinciden
// con el usuario demo responde {"token": "<JWT>"} y pone la cookie HttpOnly.
func Login(cfg Config) fiber.Handler {
	// Con usuario o contraseña vacíos, un body {} iniciaría sesión: se falla al arrancar.
	if cfg.User == "" || cfg.Password == "" || cfg.Key == nil {
		panic("auth: Login requiere User, Password y Key")
	}
	return func(c *fiber.Ctx) error {
		var in credentials
		if err := c.BodyParser(&in); err != nil {
			return fail(c, fiber.StatusBadRequest, "JSON inválido: se espera {\"username\",\"password\"}")
		}
		// Ambas comparaciones se calculan antes de decidir, para no revelar cuál falló por tiempo.
		okUser := equal(in.Username, cfg.User)
		okPass := equal(in.Password, cfg.Password)
		if !(okUser && okPass) {
			// Mismo mensaje para usuario y contraseña: no revela cuál falló.
			return fail(c, fiber.StatusUnauthorized, "credenciales inválidas")
		}

		now := time.Now()
		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
			Subject:   cfg.User,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.TTL)),
		}).SignedString(cfg.Key)
		if err != nil {
			return err // el manejador de errores responde 500 genérico
		}

		// Se emite la cookie HttpOnly para el navegador; JavaScript no puede leerla.
		c.Cookie(&fiber.Cookie{
			Name:     CookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   int(cfg.TTL.Seconds()),
			HTTPOnly: true,
			Secure:   true,
			SameSite: fiber.CookieSameSiteStrictMode,
		})

		return c.JSON(fiber.Map{"token": token})
	}
}

// Logout sobrescribe la cookie con valor vacío y Max-Age=-1 para que el navegador
// la elimine de inmediato. Conserva Path, Secure y SameSite.
func Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HTTPOnly: true,
		Secure:   true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})
	return c.SendStatus(fiber.StatusNoContent)
}

// equal compara en tiempo constante. Se comparan los SHA-256 para que la
// duración no dependa de la longitud de los textos.
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// Middleware exige un JWT RS256 válido, no caducado y con exp, verificado con pub.
// Si el token proviene de la cookie, verifica que Sec-Fetch-Site no sea cross-site (CSRF).
func Middleware(pub *rsa.PublicKey) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token, fromCookie := Token(c)
		if token == "" {
			return fail(c, fiber.StatusUnauthorized, "token inválido o ausente")
		}
		// Capa CSRF: peticiones con cookie desde otro origen se rechazan.
		if fromCookie {
			site := c.Get("Sec-Fetch-Site")
			if site != "" && site != "same-origin" {
				return fail(c, fiber.StatusForbidden, "origen no permitido")
			}
		}
		_, err := jwt.Parse(token,
			func(*jwt.Token) (any, error) { return pub, nil },
			// Solo RS256: rechaza "none" y HS256 firmado con la clave pública.
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithExpirationRequired(),
		)
		if err != nil {
			return fail(c, fiber.StatusUnauthorized, "token inválido o ausente")
		}
		return c.Next()
	}
}

// Token extrae el JWT de la petición: primero busca en "Authorization: Bearer <token>",
// y si no hay, en la cookie "qr_session". Devuelve el token y si provino de la cookie.
func Token(c *fiber.Ctx) (string, bool) {
	if token, ok := BearerToken(c); ok {
		return token, false
	}
	if cookie := c.Cookies(CookieName); cookie != "" {
		return cookie, true
	}
	return "", false
}

// BearerToken extrae el token de "Authorization: Bearer <token>".
func BearerToken(c *fiber.Ctx) (string, bool) {
	scheme, token, found := strings.Cut(c.Get(fiber.HeaderAuthorization), " ")
	if !found || scheme != "Bearer" || token == "" {
		return "", false
	}
	return token, true
}

// ParsePrivateKey lee una clave RSA privada en PEM (PKCS#8 o PKCS#1)
// codificada en base64, que es como viaja por variables de entorno.
func ParsePrivateKey(b64 string) (*rsa.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("la clave no es base64 válido")
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("la clave no contiene un bloque PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("la clave PKCS#8 no es RSA")
	}
	if rk, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return rk, nil
	}
	return nil, errors.New("no se pudo leer la clave privada RSA")
}

func fail(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}
