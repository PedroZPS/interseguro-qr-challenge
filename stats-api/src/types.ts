// Tipos compartidos; reflejan los structs de la API Go.
export type Matrix = number[][];

export interface Stats {
  max: number;
  min: number;
  sum: number;
  average: number;
  diagonalByMatrix: { q: boolean; r: boolean };
  anyDiagonal: boolean;
}
