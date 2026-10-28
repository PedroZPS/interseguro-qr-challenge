package statsclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Respuesta de ejemplo de la API Node para A=[[3,0],[4,5]].
const nodeOK = `{"max":5,"min":-0.8,"sum":13.2,"average":1.65,
"diagonalByMatrix":{"q":false,"r":false},"anyDiagonal":false}`

func TestStats_Success(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody struct{ Q, R [][]float64 }
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nodeOK))
	}))
	defer node.Close()

	q := [][]float64{{0.6, -0.8}, {0.8, 0.6}}
	r := [][]float64{{5, 4}, {0, 3}}
	c := New(node.URL, nil)

	s, err := c.Stats(context.Background(), "tok-123", q, r)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/stats" {
		t.Errorf("petición = %s %s, quiero POST /stats", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, quiero el token reenviado", gotAuth)
	}
	if len(gotBody.Q) != 2 || len(gotBody.R) != 2 || gotBody.R[0][1] != 4 {
		t.Errorf("Node recibió q=%v r=%v", gotBody.Q, gotBody.R)
	}
	if s.Max != 5 || s.Min != -0.8 || s.Sum != 13.2 || s.Average != 1.65 {
		t.Errorf("stats = %+v", s)
	}
	if s.DiagonalByMatrix.Q || s.DiagonalByMatrix.R || s.AnyDiagonal {
		t.Errorf("no debería haber diagonales: %+v", s)
	}
}

var (
	q1 = [][]float64{{1}}
	r1 = [][]float64{{1}}
)

func TestStats_NodeDown(t *testing.T) {
	node := httptest.NewServer(http.NotFoundHandler())
	url := node.URL
	node.Close() // el puerto queda cerrado: conexión rechazada

	_, err := New(url, nil).Stats(context.Background(), "tok", q1, r1)

	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, quiero ErrUnavailable", err)
	}
}

func TestStats_Timeout(t *testing.T) {
	release := make(chan struct{})
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // Node no contesta
	}))
	defer node.Close()
	defer close(release)

	c := New(node.URL, &http.Client{Timeout: 50 * time.Millisecond})

	_, err := c.Stats(context.Background(), "tok", q1, r1)

	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, quiero ErrUnavailable", err)
	}
}

func TestStats_BadResponse(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter){
		"Node responde 500": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"error interno"}`))
		},
		"Node responde 400": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"q debe ser rectangular"}`))
		},
		"200 con JSON roto": func(w http.ResponseWriter) {
			_, _ = w.Write([]byte(`<html>no soy json`))
		},
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respond(w)
			}))
			defer node.Close()

			_, err := New(node.URL, nil).Stats(context.Background(), "tok", q1, r1)

			if !errors.Is(err, ErrBadResponse) {
				t.Errorf("err = %v, quiero ErrBadResponse", err)
			}
		})
	}
}
