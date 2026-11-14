package domain

import (
	"testing"
	"time"
)

func TestCodigoQRVigente(t *testing.T) {
	secreto := []byte("secreto-de-prueba")
	ahora := time.Unix(29850000*60+30, 0)
	tok := "CE-3F9A1B2C4D5E"
	w := VentanaQR(ahora)
	casos := []struct {
		nombre  string
		codigo  string
		ventana int64
		quiere  bool
	}{
		{"código del minuto actual", CodigoDinamico(secreto, tok, w), w, true},
		{"en minúsculas", toLower(CodigoDinamico(secreto, tok, w)), w, true},
		{"minuto anterior (cambio de minuto)", CodigoDinamico(secreto, tok, w-1), w - 1, true},
		{"minuto siguiente (reloj adelantado)", CodigoDinamico(secreto, tok, w+1), w + 1, true},
		{"escrito a mano sin ventana", CodigoDinamico(secreto, tok, w), 0, true},
		{"captura de hace 3 minutos", CodigoDinamico(secreto, tok, w-3), w - 3, false},
		{"código de otra ventana", CodigoDinamico(secreto, tok, w-1), w, false},
		{"código de otro boleto", CodigoDinamico(secreto, "CE-OTRO00000000", w), w, false},
		{"generado con otro secreto (algoritmo conocido)", CodigoDinamico([]byte("otro"), tok, w), w, false},
		{"sin código", "", w, false},
	}
	for _, c := range casos {
		if got := CodigoQRVigente(secreto, tok, c.codigo, c.ventana, ahora); got != c.quiere {
			t.Errorf("%s: %v, se esperaba %v", c.nombre, got, c.quiere)
		}
	}
	desde, codigos := CodigosQR(secreto, tok, ahora)
	if desde != w || len(codigos) != CodigosPorConsulta || codigos[3] != CodigoDinamico(secreto, tok, w+3) {
		t.Fatalf("los códigos entregados al dueño deben cubrir los próximos %d minutos: %d %v", CodigosPorConsulta, desde, codigos)
	}
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
