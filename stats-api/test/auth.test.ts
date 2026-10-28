import { test } from 'node:test';
import assert from 'node:assert/strict';
import request from 'supertest';
import jwt from 'jsonwebtoken';
import { createApp } from '../src/app.ts';
import { publicKey, privateKey, attacker, sign, bearer } from './helpers.ts';

const app = createApp({ publicKey });
const body = { q: [[1]], r: [[1]] };

const b64url = (o: object) => Buffer.from(JSON.stringify(o)).toString('base64url');

async function assertUnauthorized(headers: Record<string, string>) {
  const res = await request(app).post('/stats').set(headers).send(body);
  assert.equal(res.status, 401);
  assert.equal(typeof res.body.error, 'string');
  assert.doesNotMatch(JSON.stringify(res.body), /jwt|signature|node_modules/i);
}

test('sin cabecera Authorization → 401', () => assertUnauthorized({}));

test('esquema que no es Bearer → 401', () =>
  assertUnauthorized({ Authorization: `Basic ${sign()}` }));

test('token basura → 401', () => assertUnauthorized(bearer('no.es.un-jwt')));

test('token firmado con otra clave → 401', () =>
  assertUnauthorized(bearer(sign({}, attacker.privateKey))));

test('token caducado → 401', () => assertUnauthorized(bearer(sign({ expiresIn: -10 }))));

// Firma legítima pero sin exp: un token que nunca caduca no es aceptable.
test('token legítimo sin expiración → 401', async () => {
  const noExp = jwt.sign({ sub: 'reviewer' }, privateKey, { algorithm: 'RS256' });
  await assertUnauthorized(bearer(noExp));
});

test('alg none (sin firma) → 401', () =>
  assertUnauthorized(
    bearer(`${b64url({ alg: 'none', typ: 'JWT' })}.${b64url({ sub: 'x', exp: 9999999999 })}.`),
  ));

// Ataque de confusión de algoritmo: firmar con HS256 usando la clave PÚBLICA como secreto.
test('HS256 firmado con la clave pública → 401', () =>
  assertUnauthorized(bearer(jwt.sign({ sub: 'x' }, publicKey, { algorithm: 'HS256', expiresIn: '1h' }))));

test('token válido → 200', async () => {
  const res = await request(app).post('/stats').set(bearer()).send(body);
  assert.equal(res.status, 200);
});

test('/health no exige token', async () => {
  assert.equal((await request(app).get('/health')).status, 200);
});
