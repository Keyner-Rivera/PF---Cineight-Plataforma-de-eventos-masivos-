package services

import (
	"context"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// guardiaFija acepta un único pase y sólo una vez, como CanjearPase en Redis.
type guardiaFija struct{ pase string }

func (g *guardiaFija) ExigirPase(_ context.Context, pase, _ string) error {
	if pase == "" || pase != g.pase {
		return domain.Prohibido("pase requerido")
	}
	g.pase = ""
	return nil
}

func TestValidarBoletoExigeElCodigoDinamicoVigente(t *testing.T) {
	ventas := &ventasFalsas{}
	secreto := []byte("secreto-de-prueba-de-32-bytes-ok")
	s := NuevoCuentas(nil, ventas, auditoriaMuda{}, secreto)
	ctx := context.Background()
	tok := "CE-3F9A1B2C4D5E"
	ahora := domain.VentanaQR(time.Now())

	for nombre, c := range map[string]struct {
		codigo  string
		ventana int64
	}{
		"sin código (llamada directa a la API)": {"", ahora},
		"captura de hace 5 minutos":             {domain.CodigoDinamico(secreto, tok, ahora-5), ahora - 5},
		"código inventado":                      {"ABC123", ahora},
		"algoritmo conocido sin el secreto":     {domain.CodigoDinamico([]byte("adivinado"), tok, ahora), ahora},
	} {
		if v, err := s.ValidarBoleto(ctx, tok, c.codigo, c.ventana, 1); err != nil || v.OK {
			t.Errorf("%s: debe rechazarse, fue %+v %v", nombre, v, err)
		}
	}
	if ventas.validados != 0 {
		t.Fatalf("un código rechazado no debe consumir el boleto (se marcó %d veces)", ventas.validados)
	}
	if v, err := s.ValidarBoleto(ctx, tok, domain.CodigoDinamico(secreto, tok, ahora), ahora, 1); err != nil || !v.OK || ventas.validados != 1 {
		t.Fatalf("con el código vigente se valida una vez: %+v %v (%d)", v, err, ventas.validados)
	}
}
