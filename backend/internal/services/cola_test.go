package services

import (
	"context"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// colasFalsas registra quién entró y con qué cupo se pidió avanzar; responde el estado configurado.
type colasFalsas struct {
	entraron []string
	cupo     int64
	estado   domain.EstadoCola
}

func (c *colasFalsas) EntrarCola(_ context.Context, _, usuario string, _ time.Time) error {
	c.entraron = append(c.entraron, usuario)
	return nil
}
func (c *colasFalsas) AvanzarCola(_ context.Context, _, _ string, _ time.Time, cupo int64, _, _ time.Duration) (domain.EstadoCola, error) {
	c.cupo = cupo
	return c.estado, nil
}
func (c *colasFalsas) ConfirmarTurno(context.Context, string, string, time.Time, time.Duration) error {
	return nil
}
func (c *colasFalsas) SalirCola(context.Context, string, string) error { return nil }
func (c *colasFalsas) TurnoActivo(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (c *colasFalsas) Colas(context.Context) map[string]int64                { return nil }
func (c *colasFalsas) Contar(context.Context, string, string) (int64, error) { return 0, nil }

// eventosFalsos es el inventario del preestreno y los boletos que ya tiene cada cliente.
type eventosFalsos struct {
	inv     domain.InventarioEvento
	propios map[int64]int
}

func (e *eventosFalsos) InventarioEvento(context.Context, string) (domain.InventarioEvento, error) {
	return e.inv, nil
}
func (e *eventosFalsos) BoletosDeUsuario(_ context.Context, _ int, usuarioID int64) (int, error) {
	return e.propios[usuarioID], nil
}

// retenidasFalsas simula butacas tomadas en Redis por otros clientes.
type retenidasFalsas struct {
	*candadosFalsos
	n int
}

func (r retenidasFalsas) Retenidos(context.Context, int) []string { return make([]string, r.n) }

func armarCola(libres, retenidas int, estado domain.EstadoCola) (*ColaVirtual, *colasFalsas, *eventosFalsos, *guardiaFija) {
	q := &colasFalsas{estado: estado}
	ev := &eventosFalsos{inv: domain.InventarioEvento{FuncionID: 1, Abierta: true, Libres: libres}, propios: map[int64]int{}}
	g := &guardiaFija{pase: "P1"}
	return NuevaCola(q, g, ev, retenidasFalsas{nuevosCandados(), retenidas}, 5, 3*time.Minute), q, ev, g
}

func TestPronosticoSegunButacasYPosicion(t *testing.T) {
	casos := []struct {
		disponibles int
		posicion    int64
		quiere      string
	}{
		{20, 1, "alcanza"},
		{20, 10, "alcanza"},    // 9 adelante × 2 = 18 → quedan 2
		{20, 11, "incierto"},   // 10 adelante: si todos compran 2 no queda, si compran 1 sí
		{20, 20, "incierto"},   // 19 adelante comprando 1 cada uno → queda 1
		{20, 21, "no-alcanza"}, // 20 adelante: aunque compren 1 cada uno no queda ninguna
		{0, 1, "no-alcanza"},   // todo retenido por otros en este momento
		{3, 2, "alcanza"},      // 1 adelante × 2 = 2 → queda 1
		{3, 3, "incierto"},
	}
	for _, c := range casos {
		if got := pronosticar(c.disponibles, c.posicion); got != c.quiere {
			t.Errorf("%d butacas, posición %d: %s, se esperaba %s", c.disponibles, c.posicion, got, c.quiere)
		}
	}
}

func TestCupoDeTurnosNoSuperaLasButacas(t *testing.T) {
	for _, c := range []struct {
		capacidad   int64
		disponibles int
		quiere      int64
	}{{5, 20, 5}, {5, 3, 2}, {5, 1, 1}, {5, 0, 0}, {2, 20, 2}} {
		if got := cupoTurnos(c.capacidad, c.disponibles); got != c.quiere {
			t.Errorf("capacidad %d con %d butacas: cupo %d, se esperaba %d", c.capacidad, c.disponibles, got, c.quiere)
		}
	}
}

func TestColaAgotadaNoAdmiteNiGastaElPase(t *testing.T) {
	s, q, _, g := armarCola(0, 0, domain.EstadoCola{})
	p, err := s.Entrar(context.Background(), "e1", 7, "P1")
	if err != nil || !p.Agotado {
		t.Fatalf("sin butacas la cola responde agotado: %v %+v", err, p)
	}
	if len(q.entraron) != 0 || g.pase != "P1" {
		t.Fatalf("no debe entrar a la fila ni gastar el pase anti-bots: %v, pase %q", q.entraron, g.pase)
	}
	if p, _ := s.Posicion(context.Background(), "e1", 9); !p.Agotado {
		t.Fatalf("quien ya esperaba también recibe agotado: %+v", p)
	}
}

func TestQuienYaTieneSusBoletosNoVuelveALaCola(t *testing.T) {
	s, q, ev, g := armarCola(20, 0, domain.EstadoCola{EnFila: true, Posicion: 1, EnCola: 1})
	ev.propios[7] = domain.MaxBoletosPorEvento
	if _, err := s.Entrar(context.Background(), "e1", 7, "P1"); estado(err) != 409 {
		t.Fatalf("con %d boletos ya comprados debe responder 409, fue %v", domain.MaxBoletosPorEvento, err)
	}
	if len(q.entraron) != 0 || g.pase != "P1" {
		t.Fatalf("no debe entrar ni gastar el pase: %v %q", q.entraron, g.pase)
	}
	ev.propios[8] = 1 // con un boleto todavía puede comprar el segundo
	if _, err := s.Entrar(context.Background(), "e1", 8, "P1"); err != nil || len(q.entraron) != 1 {
		t.Fatalf("con 1 boleto sí puede entrar: %v %v", err, q.entraron)
	}
}

func TestLosTurnosSeAjustanALasButacasSinTomar(t *testing.T) {
	// 6 a la venta, 4 tomadas por clientes con turno → 2 sin tomar alcanzan para 1 cliente más (máx. 2 c/u).
	s, q, _, _ := armarCola(6, 4, domain.EstadoCola{EnFila: true, Posicion: 2, EnCola: 30})
	p, err := s.Posicion(context.Background(), "e1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if q.cupo != 1 {
		t.Fatalf("con 2 butacas sin tomar el cupo de turnos es 1, fue %d", q.cupo)
	}
	if p.ButacasLibres != 2 || p.Pronostico != "incierto" {
		t.Fatalf("posición 2 con 2 butacas: incierto, fue %+v", p)
	}
}

func TestFuncionIniciadaCierraLaCola(t *testing.T) {
	s, _, ev, _ := armarCola(20, 0, domain.EstadoCola{})
	ev.inv.Abierta = false
	if p, err := s.Posicion(context.Background(), "e1", 7); err != nil || !p.Cerrada {
		t.Fatalf("con la función iniciada la cola está cerrada: %v %+v", err, p)
	}
}
