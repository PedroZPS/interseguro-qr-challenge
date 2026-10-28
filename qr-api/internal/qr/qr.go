// Package qr calcula la factorización QR reducida de una matriz con
// reflexiones de Householder. Es lógica pura: no sabe nada de HTTP.
package qr

import (
	"errors"
	"math"
)

// tolerance: un valor con |x| < tolerance se considera cero.
const tolerance = 1e-10

// MaxDim es el máximo de filas y de columnas aceptado.
const MaxDim = 100

// Errores de entrada. El handler los traducirá a 400.
var (
	ErrEmpty     = errors.New("la matriz está vacía")
	ErrIrregular = errors.New("todas las filas deben tener la misma longitud")
	ErrNonFinite = errors.New("la matriz contiene NaN o Inf")
	ErrTooLarge  = errors.New("la matriz excede el tamaño máximo")
)

// validate comprueba que a sea una matriz rectangular, finita y no demasiado grande.
func validate(a [][]float64) error {
	if len(a) == 0 || len(a[0]) == 0 {
		return ErrEmpty
	}
	if len(a) > MaxDim || len(a[0]) > MaxDim {
		return ErrTooLarge
	}
	for _, row := range a {
		if len(row) != len(a[0]) {
			return ErrIrregular
		}
		for _, x := range row {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return ErrNonFinite
			}
		}
	}
	return nil
}

// Decompose devuelve la QR reducida de a (m×n): Q es m×k y R es k×n,
// con k = min(m, n). Q tiene columnas ortonormales y R es triangular
// superior con la diagonal ≥ 0, de modo que Q·R = A y el resultado es único.
// Si la entrada no es válida devuelve un error y Q y R son nil.
func Decompose(a [][]float64) (q, r [][]float64, err error) {
	if err := validate(a); err != nil {
		return nil, nil, err
	}
	m, n := len(a), len(a[0])
	k := min(m, n)

	// Trabajamos con Q completa (m×m) y R completa (m×n); al final recortamos.
	r = copyMatrix(a)
	q = identity(m)

	for j := 0; j < k; j++ {
		// x es la columna j de R desde la diagonal hacia abajo.
		var norm float64
		for i := j; i < m; i++ {
			norm = math.Hypot(norm, r[i][j])
		}
		if norm < tolerance {
			continue // columna ya nula: no hay nada que anular
		}

		// El reflejo manda x a alpha·e1. alpha toma signo opuesto a x[0]
		// para evitar cancelación al calcular v[0] = x[0] - alpha.
		alpha := -norm
		if r[j][j] < 0 {
			alpha = norm
		}

		// v = x - alpha·e1, normalizado. H = I - 2·v·vᵀ.
		v := make([]float64, m-j)
		for i := range v {
			v[i] = r[j+i][j]
		}
		v[0] -= alpha
		var vNorm float64
		for _, x := range v {
			vNorm = math.Hypot(vNorm, x)
		}
		for i := range v {
			v[i] /= vNorm
		}

		// R ← H·R (solo filas j..m-1).
		for c := 0; c < n; c++ {
			var dot float64
			for i := range v {
				dot += v[i] * r[j+i][c]
			}
			for i := range v {
				r[j+i][c] -= 2 * v[i] * dot
			}
		}
		// Q ← Q·H (solo columnas j..m-1). Al terminar, Q = H₁·H₂·…·Hₖ y Q·R = A.
		for row := 0; row < m; row++ {
			var dot float64
			for i := range v {
				dot += q[row][j+i] * v[i]
			}
			for i := range v {
				q[row][j+i] -= 2 * dot * v[i]
			}
		}
	}

	// Recorte a la forma reducida.
	q = takeColumns(q, k)
	r = r[:k]

	// Debajo de la diagonal solo queda ruido de redondeo: lo dejamos en 0 exacto.
	for i := 0; i < k; i++ {
		for j := 0; j < i && j < n; j++ {
			r[i][j] = 0
		}
	}

	// Normalizar signos: si R[j][j] < 0, negamos la columna j de Q y la fila j de R.
	// Se niega como 0 - x y no como -x para que un 0 siga siendo +0 (y no "-0" en el JSON).
	for j := 0; j < k; j++ {
		if r[j][j] < 0 {
			for i := range q {
				q[i][j] = 0 - q[i][j]
			}
			for c := range r[j] {
				r[j][c] = 0 - r[j][c]
			}
		}
	}

	return q, r, nil
}

func copyMatrix(a [][]float64) [][]float64 {
	c := make([][]float64, len(a))
	for i := range a {
		c[i] = append([]float64(nil), a[i]...)
	}
	return c
}

func identity(n int) [][]float64 {
	id := make([][]float64, n)
	for i := range id {
		id[i] = make([]float64, n)
		id[i][i] = 1
	}
	return id
}

// takeColumns devuelve una copia con solo las primeras k columnas de a.
func takeColumns(a [][]float64, k int) [][]float64 {
	out := make([][]float64, len(a))
	for i := range a {
		out[i] = append([]float64(nil), a[i][:k]...)
	}
	return out
}
