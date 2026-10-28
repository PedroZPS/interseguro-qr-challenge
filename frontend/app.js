// Frontend sin dependencias: login → matriz → POST /api/qr → resultado.
// La sesión viaja en una cookie HttpOnly: JavaScript no manipula el token.
// Todo texto que viene de la API se pinta con textContent, nunca con innerHTML.

const MAX_DIM = 100; // mismo límite que la API Go
const EXAMPLE = '3 0\n4 5';

const $ = (id) => document.getElementById(id);

class ApiError extends Error {
  constructor(status, message, retryAfter) {
    super(message);
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

async function post(path, body) {
  let res;
  try {
    res = await fetch(path, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, 'No se pudo conectar con el servidor. Revisa tu conexión e inténtalo de nuevo.');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.error || 'Error inesperado.', res.headers.get('Retry-After'));
  return data;
}

// ---- Vistas ----

function showView(name) {
  $('login-view').hidden = name !== 'login';
  $('app-view').hidden = name !== 'app';
}

function setMessage(id, text, kind) {
  const el = $(id);
  el.textContent = text || '';
  el.className = kind || '';
}

async function logout(message) {
  try {
    await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' });
  } catch {
    // Si falla la red, continuar y mostrar login de todas formas.
  }
  $('result').hidden = true;
  $('result').replaceChildren();
  setMessage('qr-message', '');
  setMessage('login-message', message, message ? 'info' : '');
  showView('login');
}

// ---- Entrada de la matriz ----

// Convierte el texto en una matriz de números; si no es válido lanza un Error con el motivo.
function parseMatrix(text) {
  const lines = text.split('\n').map((l) => l.trim()).filter((l) => l !== '');
  if (lines.length === 0) throw new Error('Escribe al menos una fila de valores.');
  if (lines.length > MAX_DIM) throw new Error(`La matriz tiene ${lines.length} filas; el máximo es ${MAX_DIM}.`);

  const rows = lines.map((line, i) =>
    line.split(/[\s,;]+/).map((token) => {
      const value = Number(token);
      if (!Number.isFinite(value)) throw new Error(`«${token}» (fila ${i + 1}) no es un número.`);
      return value;
    }),
  );
  const cols = rows[0].length;
  if (cols > MAX_DIM) throw new Error(`La matriz tiene ${cols} columnas; el máximo es ${MAX_DIM}.`);
  rows.forEach((row, i) => {
    if (row.length !== cols) {
      throw new Error(`La fila ${i + 1} tiene ${row.length} ${row.length === 1 ? 'valor' : 'valores'} y la primera tiene ${cols}. Todas deben tener los mismos.`);
    }
  });
  return rows;
}

// ---- Resultado ----

// Redondea a 6 cifras significativas y evita mostrar ruido numérico como -1e-17.
function fmt(x) {
  if (Math.abs(x) < 1e-12) return '0';
  return String(Number(x.toPrecision(6)));
}

function el(tag, props = {}, children = []) {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}

// Dibuja una matriz con corchetes. belowDiagonalDim atenúa los ceros bajo la diagonal (R).
function matrixFigure(name, m, belowDiagonalDim) {
  const table = el('table', {}, [
    el('caption', { className: 'sr-only', textContent: `${name}, ${m.length} filas por ${m[0].length} columnas` }),
    ...m.map((row, i) =>
      el('tr', {}, row.map((value, j) =>
        el('td', { textContent: fmt(value), title: String(value), className: belowDiagonalDim && i > j ? 'below' : '' }),
      )),
    ),
  ]);
  return el('figure', {}, [
    el('div', { className: 'matrix' }, [el('div', { className: 'scroll' }, [table])]),
    el('figcaption', { textContent: `${name} · ${m.length}×${m[0].length}` }),
  ]);
}

// Mayor diferencia absoluta entre Q·R y A: comprueba en el navegador que la factorización es correcta.
function reconstructionError(a, q, r) {
  let worst = 0;
  for (let i = 0; i < a.length; i++) {
    for (let j = 0; j < a[0].length; j++) {
      let sum = 0;
      for (let p = 0; p < r.length; p++) sum += q[i][p] * r[p][j];
      worst = Math.max(worst, Math.abs(sum - a[i][j]));
    }
  }
  return worst;
}

// Un operador y la matriz que le sigue forman un grupo: si la ecuación se parte en
// dos líneas, el operador nunca queda solo al final de la primera.
const term = (op, figure) =>
  el('div', { className: 'term' }, [el('span', { className: 'op', textContent: op, ariaHidden: 'true' }), figure]);

const yesNo = (b) => (b ? 'Sí' : 'No');

function renderResult(a, { q, r, stats }) {
  const err = reconstructionError(a, q, r);
  const rows = [
    ['Máximo', fmt(stats.max)],
    ['Mínimo', fmt(stats.min)],
    ['Suma', fmt(stats.sum)],
    ['Promedio', fmt(stats.average)],
    ['¿Q es diagonal?', yesNo(stats.diagonalByMatrix.q)],
    ['¿R es diagonal?', yesNo(stats.diagonalByMatrix.r)],
    ['¿Alguna es diagonal?', yesNo(stats.anyDiagonal)],
  ];

  $('result').replaceChildren(
    el('h2', { textContent: 'Resultado' }),
    el('div', { className: 'equation' }, [
      matrixFigure('A', a),
      term('=', matrixFigure('Q', q)),
      term('×', matrixFigure('R', r, true)),
    ]),
    el('p', {
      className: 'check',
      textContent: `Comprobación en tu navegador: el mayor error de Q·R − A es ${err === 0 ? '0' : err.toExponential(1)}.`,
    }),
    el('h2', { textContent: 'Estadísticas de Q y R juntas' }),
    el('dl', {}, rows.flatMap(([k, v]) => [el('dt', { textContent: k }), el('dd', { textContent: v })])),
  );
  $('result').hidden = false;
}

// ---- Eventos ----

function busy(form, isBusy, label) {
  const button = form.querySelector('button[type="submit"]');
  button.disabled = isBusy;
  if (label) button.textContent = isBusy ? label.busy : label.idle;
  form.setAttribute('aria-busy', String(isBusy));
}

$('login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  setMessage('login-message', '');
  busy(form, true, { busy: 'Entrando…', idle: 'Iniciar sesión' });
  try {
    await post('/auth/login', {
      username: $('username').value.trim(),
      password: $('password').value,
    });
    $('password').value = '';
    showView('app');
  } catch (err) {
    if (err.status === 401) setMessage('login-message', 'Usuario o contraseña incorrectos.');
    else if (err.status === 429) setMessage('login-message', `Demasiados intentos. Espera ${err.retryAfter || 'un minuto'} s e inténtalo de nuevo.`);
    else setMessage('login-message', err.message);
  } finally {
    busy(form, false, { busy: 'Entrando…', idle: 'Iniciar sesión' });
  }
});

$('qr-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  setMessage('qr-message', '');
  let matrix;
  try {
    matrix = parseMatrix($('matrix').value);
  } catch (err) {
    setMessage('qr-message', err.message);
    return;
  }

  busy(form, true, { busy: 'Calculando…', idle: 'Calcular QR' });
  try {
    renderResult(matrix, await post('/api/qr', { matrix }));
  } catch (err) {
    if (err.status === 401) logout('Tu sesión caducó. Inicia sesión de nuevo.');
    else if (err.status === 429) setMessage('qr-message', `Demasiadas peticiones. Espera ${err.retryAfter || 'un minuto'} s e inténtalo de nuevo.`);
    else setMessage('qr-message', err.message);
  } finally {
    busy(form, false, { busy: 'Calculando…', idle: 'Calcular QR' });
  }
});

$('example').addEventListener('click', () => { $('matrix').value = EXAMPLE; });
$('logout').addEventListener('click', () => { logout(); });

// Al cargar: comprueba si ya hay una sesión activa en la cookie.
// Mientras responde, ambas vistas se mantienen ocultas para evitar parpadeos.
async function checkSession() {
  try {
    const res = await fetch('/auth/session', { credentials: 'same-origin' });
    if (res.status === 204) {
      showView('app');
      return;
    }
  } catch {
    /* si hay fallo de red, se muestra el login */
  }
  showView('login');
}

checkSession();
