package qr

import (
	"errors"
	"math"
	"testing"
)

// testEps es la tolerancia para comparar floats en los tests.
// Es más holgada que la del código (1e-10) porque el redondeo se acumula.
const testEps = 1e-9

func assertMatrixNear(t *testing.T, name string, got, want [][]float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: filas = %d, quiero %d", name, len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("%s: fila %d tiene %d columnas, quiero %d", name, i, len(got[i]), len(want[i]))
		}
		for j := range want[i] {
			if math.Abs(got[i][j]-want[i][j]) > testEps {
				t.Errorf("%s[%d][%d] = %v, quiero %v", name, i, j, got[i][j], want[i][j])
			}
		}
	}
}

// assertQRProperties verifica las propiedades que definen una QR reducida
// válida, sin depender de valores exactos: forma, Q·R ≈ A, QᵀQ ≈ I,
// R triangular superior y diag(R) ≥ 0.
func assertQRProperties(t *testing.T, a [][]float64) {
	t.Helper()
	m, n := len(a), len(a[0])
	k := min(m, n)

	q, r, err := Decompose(a)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if len(q) != m || len(q[0]) != k {
		t.Fatalf("Q es %dx%d, quiero %dx%d", len(q), len(q[0]), m, k)
	}
	if len(r) != k || len(r[0]) != n {
		t.Fatalf("R es %dx%d, quiero %dx%d", len(r), len(r[0]), k, n)
	}

	// Q·R ≈ A
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			var sum float64
			for p := 0; p < k; p++ {
				sum += q[i][p] * r[p][j]
			}
			if math.Abs(sum-a[i][j]) > testEps {
				t.Errorf("(Q·R)[%d][%d] = %v, quiero %v", i, j, sum, a[i][j])
			}
		}
	}

	// QᵀQ ≈ I (k×k)
	for i := 0; i < k; i++ {
		for j := 0; j < k; j++ {
			var dot float64
			for p := 0; p < m; p++ {
				dot += q[p][i] * q[p][j]
			}
			want := 0.0
			if i == j {
				want = 1
			}
			if math.Abs(dot-want) > testEps {
				t.Errorf("(QᵀQ)[%d][%d] = %v, quiero %v", i, j, dot, want)
			}
		}
	}

	// R triangular superior y diagonal ≥ 0
	for i := 0; i < k; i++ {
		for j := 0; j < n; j++ {
			if j < i && r[i][j] != 0 {
				t.Errorf("R[%d][%d] = %v, debería ser 0 (bajo la diagonal)", i, j, r[i][j])
			}
		}
		if r[i][i] < 0 {
			t.Errorf("R[%d][%d] = %v, la diagonal debe ser ≥ 0", i, i, r[i][i])
		}
	}
}

func TestDecompose_Properties(t *testing.T) {
	cases := []struct {
		name string
		a    [][]float64
	}{
		{"2x2", [][]float64{{1, 2}, {3, 4}}},
		{"3x2", [][]float64{{1, 2}, {3, 4}, {5, 6}}},
		{"4x3", [][]float64{{2, -1, 0}, {1, 3, 4}, {-2, 5, 1}, {0, 1, -3}}},
		{"2x3 (m<n)", [][]float64{{1, 2, 3}, {4, 5, 6}}},
		{"1x3 (m<n)", [][]float64{{3, 4, 12}}},
		// Casos límite: rango deficiente y columnas nulas (Householder salta la columna).
		{"cero", [][]float64{{0, 0}, {0, 0}}},
		{"rango 1", [][]float64{{1, 2}, {2, 4}}},
		{"columna nula", [][]float64{{0, 1}, {0, 2}}},
		{"filas nulas", [][]float64{{1, 2, 3}, {0, 0, 0}, {0, 0, 0}}},
		{"1x1 negativo", [][]float64{{-7}}},
		{"3x1", [][]float64{{1}, {2}, {2}}},
		{"ya triangular", [][]float64{{2, 1}, {0, 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertQRProperties(t, tc.a) })
	}
}

// Ejemplo del enunciado: A=[[3,0],[4,5]] con diagonal de R positiva.
func TestDecompose_FixedExample(t *testing.T) {
	a := [][]float64{{3, 0}, {4, 5}}

	q, r, err := Decompose(a)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	assertMatrixNear(t, "Q", q, [][]float64{{0.6, -0.8}, {0.8, 0.6}})
	assertMatrixNear(t, "R", r, [][]float64{{5, 4}, {0, 3}})
}

func TestDecompose_InvalidInput(t *testing.T) {
	big := make([][]float64, MaxDim+1)
	for i := range big {
		big[i] = []float64{1}
	}

	cases := []struct {
		name string
		a    [][]float64
		want error
	}{
		{"nil", nil, ErrEmpty},
		{"sin filas", [][]float64{}, ErrEmpty},
		{"fila vacía", [][]float64{{}}, ErrEmpty},
		{"filas irregulares", [][]float64{{1, 2}, {3}}, ErrIrregular},
		{"NaN", [][]float64{{1, math.NaN()}, {3, 4}}, ErrNonFinite},
		{"+Inf", [][]float64{{1, 2}, {math.Inf(1), 4}}, ErrNonFinite},
		{"-Inf", [][]float64{{1, 2}, {3, math.Inf(-1)}}, ErrNonFinite},
		{"demasiadas filas", big, ErrTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, r, err := Decompose(tc.a)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, quiero %v", err, tc.want)
			}
			if q != nil || r != nil {
				t.Errorf("con error, Q y R deben ser nil")
			}
		})
	}
}

// -0 es válido en float, pero en JSON sale como "-0" y confunde. Los ceros
// de Q y R deben ser +0 (aquí R[1][1] < 0 antes de normalizar).
func TestDecompose_NoNegativeZero(t *testing.T) {
	q, r, err := Decompose([][]float64{{3, 0}, {4, 5}})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	for name, m := range map[string][][]float64{"Q": q, "R": r} {
		for i := range m {
			for j := range m[i] {
				if m[i][j] == 0 && math.Signbit(m[i][j]) {
					t.Errorf("%s[%d][%d] es -0", name, i, j)
				}
			}
		}
	}
}
