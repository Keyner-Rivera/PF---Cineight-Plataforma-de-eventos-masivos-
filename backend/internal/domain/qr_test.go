package domain

import (
	"testing"
	"time"
)

// Valores calculados con codigoDinamico() del frontend: ambos lados deben dar exactamente el mismo código.
func TestCodigoDinamicoIgualAlDelFrontend(t *testing.T) {
	for _, c := range []struct {
		token   string
		ventana int64
		quiere  string
	}{
		{"CE-3F9A1B2C4D5E", 29850000, "CF0H8D"},
		{"CE-AAAAAAAAAAAA", 1, "HJ07HH"},
		{"TQ-0123456789AB", 29851234, "JRJNTJ"},
	} {
		if got := CodigoDinamico(c.token, c.ventana); got != c.quiere {
			t.Errorf("%s/%d: %s, el frontend genera %s", c.token, c.ventana, got, c.quiere)
		}
	}
}

func TestCodigoQRVigente(t *testing.T) {
	ahora := time.Unix(29850000*60+30, 0) // a mitad de la ventana 29850000
	tok := "CE-3F9A1B2C4D5E"
	w := VentanaQR(ahora)
	casos := []struct {
		nombre  string
		codigo  string
		ventana int64
		quiere  bool
	}{
		{"código del minuto actual", CodigoDinamico(tok, w), w, true},
		{"en minúsculas", "cf0h8d", w, true},
		{"minuto anterior (cambio de minuto)", CodigoDinamico(tok, w-1), w - 1, true},
		{"minuto siguiente (reloj adelantado)", CodigoDinamico(tok, w+1), w + 1, true},
		{"captura de hace 3 minutos", CodigoDinamico(tok, w-3), w - 3, false},
		{"código de otra ventana", CodigoDinamico(tok, w-1), w, false},
		{"código de otro boleto", CodigoDinamico("CE-OTRO00000000", w), w, false},
		{"sin código", "", w, false},
	}
	for _, c := range casos {
		if got := CodigoQRVigente(tok, c.codigo, c.ventana, ahora); got != c.quiere {
			t.Errorf("%s: %v, se esperaba %v", c.nombre, got, c.quiere)
		}
	}
}
