import { test } from 'node:test';
import assert from 'node:assert/strict';
import request from 'supertest';
import { createApp } from '../src/app.ts';
import { publicKey, bearer } from './helpers.ts';

const app = createApp({ publicKey });

test('GET /health responde 200', async () => {
  const res = await request(app).get('/health');

  assert.equal(res.status, 200);
  assert.deepEqual(res.body, { status: 'ok' });
});

test('POST /stats devuelve las estadísticas de Q y R', async () => {
  const res = await request(app)
    .post('/stats')
    .set(bearer())
    .send({ q: [[0.6, -0.8], [0.8, 0.6]], r: [[5, 4], [0, 3]] });

  assert.equal(res.status, 200);
  assert.equal(res.body.max, 5);
  assert.equal(res.body.min, -0.8);
  assert.ok(Math.abs(res.body.sum - 13.2) < 1e-9);
  assert.ok(Math.abs(res.body.average - 1.65) < 1e-9);
  assert.deepEqual(res.body.diagonalByMatrix, { q: false, r: false });
  assert.equal(res.body.anyDiagonal, false);
});

const invalidBodies = {
  'sin r': { q: [[1]] },
  'sin q': { r: [[1]] },
  'q no es arreglo': { q: 5, r: [[1]] },
  'matriz vacía': { q: [], r: [[1]] },
  'fila vacía': { q: [[]], r: [[1]] },
  'filas irregulares': { q: [[1, 2], [3]], r: [[1]] },
  'valor no numérico': { q: [[1, 'a']], r: [[1]] },
  'valor null': { q: [[1, null]], r: [[1]] },
};

for (const [name, body] of Object.entries(invalidBodies)) {
  test(`POST /stats responde 400 con entrada inválida: ${name}`, async () => {
    const res = await request(app).post('/stats')
    .set(bearer()).send(body);

    assert.equal(res.status, 400);
    assert.equal(typeof res.body.error, 'string');
  });
}

test('POST /stats responde 400 con JSON malformado, sin filtrar el stack', async () => {
  const res = await request(app)
    .post('/stats')
    .set(bearer())
    .set('Content-Type', 'application/json')
    .send('{"q": [[1,');

  assert.equal(res.status, 400);
  assert.equal(typeof res.body.error, 'string');
  assert.doesNotMatch(JSON.stringify(res.body), /node_modules|at \w+/);
});

test('ruta desconocida responde 404 en JSON', async () => {
  const res = await request(app).get('/nada');

  assert.equal(res.status, 404);
  assert.equal(typeof res.body.error, 'string');
});
