package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// cuentasMemoria es un doble de Cuentas: sólo importa qué correos existen y cuántas cuentas se crean.
type cuentasMemoria struct {
	existentes map[string]bool
	creadas    int
}

func (c *cuentasMemoria) ExisteCliente(_ context.Context, correo string) (bool, error) {
	return c.existentes[correo], nil
}
func (c *cuentasMemoria) VincularCliente(_ context.Context, correo, _ string) (int64, int, error) {
	if !c.existentes[correo] {
		c.existentes[correo] = true
		c.creadas++
	}
	return 1, domain.BonoBienvenida, nil
}
func (c *cuentasMemoria) Cuenta(context.Context, int64) (domain.Cuenta, error) {
	return domain.Cuenta{}, nil
}
func (c *cuentasMemoria) AcreditarTrivia(context.Context, int64, string, int) (bool, int, error) {
	return false, 0, nil
}
func (c *cuentasMemoria) Metricas(context.Context) (domain.Metricas, error) {
	return domain.Metricas{}, nil
}
func (c *cuentasMemoria) HistorialClientes(context.Context) ([]domain.HistorialCliente, error) {
	return nil, nil
}

// guardiaFija acepta un único pase y sólo una vez, como CanjearPase en Redis.
type guardiaFija struct{ pase string }

func (g *guardiaFija) ExigirPase(_ context.Context, pase, _ string) error {
	if pase == "" || pase != g.pase {
		return domain.Prohibido("pase requerido")
	}
	g.pase = ""
	return nil
}

func TestSesionExigePaseSoloParaCuentasNuevas(t *testing.T) {
	ctx := context.Background()
	repo := &cuentasMemoria{existentes: map[string]bool{"ana.lopez@correo.com": true}}
	s := NuevoCuentas(repo, nil, nil, &guardiaFija{pase: "P1"})

	if _, _, err := s.Sesion(ctx, "ana.lopez@correo.com", "Ana López", ""); err != nil {
		t.Fatalf("una cuenta existente no debería pedir pase: %v", err)
	}

	_, _, err := s.Sesion(ctx, "bot.nuevo@correo.com", "Bot Nuevo", "")
	var e *domain.Error
	if !errors.As(err, &e) || e.Estado != 403 {
		t.Fatalf("crear una cuenta sin pase debería responder 403, obtuve %v", err)
	}

	if _, _, err := s.Sesion(ctx, "luis.perez@correo.com", "Luis Pérez", "P1"); err != nil {
		t.Fatalf("con pase válido debería crear la cuenta: %v", err)
	}
	if _, _, err := s.Sesion(ctx, "otra.cuenta@correo.com", "Otra Cuenta", "P1"); err == nil {
		t.Fatal("un pase ya usado no debería servir para otra cuenta")
	}
	if repo.creadas != 1 {
		t.Fatalf("se esperaba 1 cuenta creada, hubo %d", repo.creadas)
	}
}

func TestValidarBoletoExigeElCodigoDinamicoVigente(t *testing.T) {
	ventas := &ventasFalsas{}
	s := NuevoCuentas(nil, ventas, auditoriaMuda{}, nil)
	ctx := context.Background()
	tok := "CE-3F9A1B2C4D5E"
	ahora := domain.VentanaQR(time.Now())

	for nombre, c := range map[string]struct {
		codigo  string
		ventana int64
	}{
		"sin código (llamada directa a la API)": {"", ahora},
		"captura de hace 5 minutos":             {domain.CodigoDinamico(tok, ahora-5), ahora - 5},
		"código inventado":                      {"ABC123", ahora},
	} {
		if v, err := s.ValidarBoleto(ctx, tok, c.codigo, c.ventana); err != nil || v.OK {
			t.Errorf("%s: debe rechazarse, fue %+v %v", nombre, v, err)
		}
	}
	if ventas.validados != 0 {
		t.Fatalf("un código rechazado no debe consumir el boleto (se marcó %d veces)", ventas.validados)
	}
	if v, err := s.ValidarBoleto(ctx, tok, domain.CodigoDinamico(tok, ahora), ahora); err != nil || !v.OK || ventas.validados != 1 {
		t.Fatalf("con el código vigente se valida una vez: %+v %v (%d)", v, err, ventas.validados)
	}
}
