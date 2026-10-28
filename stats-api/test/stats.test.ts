import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeStats } from '../src/stats.ts';

const near = (got: number, want: number, msg: string) =>
  assert.ok(Math.abs(got - want) < 1e-9, `${msg}: ${got} != ${want}`);

// Ejemplo del plan: salida de la QR de A=[[3,0],[4,5]].
test('ejemplo 2x2: max, min, suma, promedio y sin diagonales', () => {
  const q = [[0.6, -0.8], [0.8, 0.6]];
  const r = [[5, 4], [0, 3]];

  const s = computeStats(q, r);

  assert.equal(s.max, 5);
  near(s.min, -0.8, 'min');
  near(s.sum, 13.2, 'sum');
  near(s.average, 1.65, 'average');
  assert.deepEqual(s.diagonalByMatrix, { q: false, r: false });
  assert.equal(s.anyDiagonal, false);
});

test('diagonal con ruido de 1e-15 cuenta como diagonal', () => {
  const q = [[0.6, -0.8], [0.8, 0.6]];
  const r = [[5, 1e-15], [-1e-15, 3]];

  const s = computeStats(q, r);

  assert.deepEqual(s.diagonalByMatrix, { q: false, r: true });
  assert.equal(s.anyDiagonal, true);
});

test('ruido por encima de la tolerancia no es diagonal', () => {
  const s = computeStats([[1, 0], [0, 1]], [[5, 1e-9], [0, 3]]);

  assert.deepEqual(s.diagonalByMatrix, { q: true, r: false });
});

test('matrices rectangulares: diagonal ignora la forma', () => {
  // 3x2 con ceros fuera de la diagonal principal
  const s = computeStats([[1, 0], [0, 1], [0, 0]], [[2, 3], [0, 4]]);

  assert.deepEqual(s.diagonalByMatrix, { q: true, r: false });
});
