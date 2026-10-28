# Reto técnico: factorización QR con Go y Node

[![CI](https://github.com/PedroZPS/interseguro-qr-challenge/actions/workflows/ci.yml/badge.svg)](https://github.com/PedroZPS/interseguro-qr-challenge/actions/workflows/ci.yml)

Dos APIs que se comunican por HTTP:

- **qr-api** (Go + Fiber): recibe una matriz rectangular, calcula su **factorización QR** y le pide a la otra API las estadísticas de Q y R.
- **stats-api** (Node + Express + TypeScript): recibe Q y R y calcula máximo, mínimo, suma, promedio y si alguna matriz es diagonal.
- **frontend**: una página sin dependencias, servida por la propia API Go. Muestra el resultado de ambas APIs (Q y R de Go, estadísticas de Node) a través de `qr-api`.

Despliegue de la demo: **https://qr-api.delightfulbush-8f79e94c.eastus.azurecontainerapps.io** (Azure Container Apps). Usuario y contraseña se envían por correo.

## Arquitectura

```
navegador ──► qr-api (Go, público) ──► stats-api (Node, solo red interna)
              login, JWT, QR             valida el mismo JWT, estadísticas
```

- Solo `qr-api` es accesible desde internet. `stats-api` tiene ingress interno en Azure y no publica puertos en `docker compose`.
- Go emite el token y lo reenvía a Node, que lo valida por su cuenta (defensa en profundidad).

## Cómo ejecutarlo

Requisitos: Docker y Docker Compose.

```bash
cp .env.example .env
# Edita .env: define DEMO_PASSWORD y genera el par de claves JWT (instrucciones dentro del archivo).
docker compose up --build
```

Abre http://localhost:8080 o usa `curl`:

```bash
# 1. Obtener un token (la contraseña es la de DEMO_PASSWORD)
TOKEN=$(curl -s -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"reviewer","password":"<tu-contraseña>"}' | jq -r .token)

# 2. Factorizar una matriz
curl -s -X POST localhost:8080/api/qr \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"matrix":[[3,0],[4,5]]}'
```

Respuesta (Q y R se muestran redondeadas):

```json
{
  "q": [[0.6, -0.8], [0.8, 0.6]],
  "r": [[5, 4], [0, 3]],
  "stats": {
    "max": 5, "min": -0.8, "sum": 13.2, "average": 1.65,
    "diagonalByMatrix": { "q": false, "r": false },
    "anyDiagonal": false
  }
}
```

### Tests

```bash
cd qr-api    && go vet ./... && go test ./...
cd stats-api && pnpm install && pnpm test
```

## API

| Método y ruta | Auth | Descripción |
|---|---|---|
| `GET /health` | no | Estado del servicio |
| `POST /auth/login` | no | `{"username","password"}` → `{"token"}` y cookie `qr_session` (JWT RS256, 1 h) |
| `POST /auth/logout` | no | Borra la cookie de sesión (`qr_session`, Max-Age=-1) → 204 |
| `GET /auth/session` | Bearer o cookie | Estado de la sesión actual → 204 si válida, 401 si no |
| `POST /api/qr` | Bearer o cookie | `{"matrix": [[...]]}` → `{"q","r","stats"}` |

Errores, siempre `{"error": "mensaje"}` y sin detalles internos:

| Código | Cuándo |
|---|---|
| 400 | Matriz vacía, irregular, valores no numéricos, más de 100×100, JSON inválido |
| 401 | Sin token, token inválido/caducado o credenciales incorrectas |
| 403 | Origen no permitido (CSRF: petición con cookie desde otro origen según `Sec-Fetch-Site`) |
| 413 | Body de más de 1 MB |
| 415 | Tipo de medio no soportado (se exige `Content-Type: application/json` en `/api/qr`) |
| 429 | Demasiadas peticiones (30/min en `/api`, 5/min en login, por IP) |
| 502 / 503 | `stats-api` respondió con un error / no está disponible |
| 500 | Error inesperado (mensaje genérico) |

## Decisiones

- **El enunciado dice "rotación" y también "factorización QR".** Se contradice, así que implementé la **QR**. Q es ortogonal (una rotación o reflexión), por lo que cubre la idea de rotación.
- **Householder propio**, sin librerías de álgebra lineal. Devuelve la QR *reducida*, con k = min(m, n): Q es m×k y R es k×n. Soporta matrices con más columnas que filas.
- **Signos normalizados:** la diagonal de R es siempre ≥ 0 (si R[k][k] < 0, se niega la columna k de Q y la fila k de R). La factorización es única y las estadísticas, deterministas.
- **Tolerancia 1e-10:** un valor con |x| < 1e-10 se considera cero (en la factorización y en el chequeo de diagonal). Evita que el ruido de punto flotante (p. ej. 1e-15) impida detectar una matriz diagonal.
- **Estadísticas sobre Q y R juntas.** El enunciado habla de "las matrices devueltas", así que máximo, mínimo, suma y promedio se calculan sobre todos sus valores. La diagonal se informa por matriz (`diagonalByMatrix`) y de forma global (`anyDiagonal`). En una matriz rectangular, "diagonal" significa que todo elemento con i ≠ j es cero.
- **JWT con RS256** en vez de HS256: solo Go tiene la clave privada y emite tokens; Node solo conoce la pública, así que aunque se comprometa no puede fabricar tokens. Se fija el algoritmo, se exige `exp` y hay tests contra `alg: none`, claves ajenas, tokens caducados y el ataque de confusión HS256 con la clave pública.
- **Sesión con cookie HttpOnly y protección CSRF:** en el frontend el token viaja en una cookie `HttpOnly`, `Secure` y `SameSite=Strict`. JavaScript no puede leer el token (protege contra exfiltración por XSS) y la sesión se comparte entre pestañas. Para clientes de API y `curl`, se mantiene soporte completo de `Authorization: Bearer`.
- **El frontend consume ambas APIs a través de Go.** El enunciado pide un frontend que consuma las dos; aquí el navegador llama a `qr-api` y esta a `stats-api`, y la respuesta trae Q, R y las estadísticas. Así Node queda solo en la red interna (sin exponerlo a internet) y el navegador maneja un único origen.
- **Frontend servido por la misma API:** no hace falta CORS (queda cerrado) y la CSP puede ser estricta (`default-src 'self'`).
- **pnpm** en vez de npm: el lockfile instala exactamente lo mismo en local, CI y Docker, y su `node_modules` estricto impide importar paquetes que no estén declarados en `package.json`. La versión se fija con `packageManager` y la instala corepack.
- **TDD:** los tests de QR verifican propiedades (Q·R ≈ A, QᵀQ ≈ I, R triangular superior, diagonal ≥ 0), no valores de una librería.

## Seguridad

- Claves y contraseña por variables de entorno; `.env` está en `.gitignore`. En Azure son *secrets* de Container Apps y Node solo recibe la clave pública.
- Cookie `HttpOnly`, `Secure`, `SameSite=Strict` y protección CSRF en tres capas: (1) `SameSite=Strict`; (2) si el token viene por cookie, se comprueba que `Sec-Fetch-Site` sea `same-origin` (necesario porque `azurecontainerapps.io` no está en la Public Suffix List y otras apps en el mismo dominio base compartirían "sitio"); (3) `POST /api/qr` rechaza cualquier Content-Type distinto de `application/json` con 415, impidiendo envíos forzados desde formularios HTML tradicionales.
- Contenedores sin privilegios: Go sobre `distroless/static:nonroot` y Node con `USER node`.
- Límite de body (1 MB), de tamaño de matriz (100×100), rate limit, timeouts de lectura y escritura, `recover` y errores en JSON sin stack traces.
- Comparación de credenciales en tiempo constante y el mismo mensaje para usuario y contraseña incorrectos.

## Limitaciones conocidas

- El rate limit usa `X-Forwarded-For` (detrás del ingress de Azure). Un cliente podría falsearlo para esquivarlo; en producción habría que fijar los proxies de confianza. Además, el contador vive en memoria, por lo que solo vale para una réplica.
- Un único usuario demo y tokens de 1 h sin renovación.
- Al cerrar sesión se borra la cookie del navegador, pero el JWT sigue siendo válido hasta que caduca su TTL (1 h); no hay lista de revocación en memoria ni base de datos.
- El registro de imágenes de Azure usa el usuario administrador; una instalación más cuidada usaría identidad administrada.
- La demo corre con una réplica mínima por servicio para evitar arranques en frío durante la revisión.

## Estructura

```
qr-api/      Go + Fiber   cmd/server, internal/{qr,handlers,statsclient,auth,server}
stats-api/   Node + Express + TypeScript   src/{app,auth,stats,types,server}.ts
frontend/    index.html, app.js, styles.css
docker-compose.yml   .env.example   .github/workflows/ci.yml
```
