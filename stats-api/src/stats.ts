import type { Matrix, Stats } from './types.ts';

// Lógica pura de estadísticas: no sabe nada de HTTP.

// Un valor con |x| < TOLERANCE se considera cero (igual que en la API Go).
const TOLERANCE = 1e-10;

// Una matriz es diagonal si todo elemento fuera de la diagonal es ~0.
function isDiagonal(m: Matrix): boolean {
  for (let i = 0; i < m.length; i++) {
    for (let j = 0; j < m[i].length; j++) {
      if (i !== j && Math.abs(m[i][j]) >= TOLERANCE) return false;
    }
  }
  return true;
}

// Estadísticas sobre Q y R juntas, en una sola pasada.
export function computeStats(q: Matrix, r: Matrix): Stats {
  let max = -Infinity;
  let min = Infinity;
  let sum = 0;
  let count = 0;

  for (const m of [q, r]) {
    for (const row of m) {
      for (const x of row) {
        if (x > max) max = x;
        if (x < min) min = x;
        sum += x;
        count++;
      }
    }
  }

  const diagonalByMatrix = { q: isDiagonal(q), r: isDiagonal(r) };
  return {
    max,
    min,
    sum,
    average: sum / count,
    diagonalByMatrix,
    anyDiagonal: diagonalByMatrix.q || diagonalByMatrix.r,
  };
}
