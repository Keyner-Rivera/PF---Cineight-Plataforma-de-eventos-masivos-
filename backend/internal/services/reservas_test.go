package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// Dobles de prueba en memoria: las pruebas del servicio de reservas no abren conexiones a PostgreSQL ni a Redis.

// catalogoFalso conoce una sola función (id 1, código f1).
type catalogoFalso struct {
	evento   *string
	filas    int // 0 = sin límite; si no, la sala tiene filas A…'A'+filas-1
	vendidos map[string]bool
	iniciada bool
}

func (c *catalogoFalso) Funciones(context.Context) ([]domain.Funcion, error) { return nil, nil }
func (c *catalogoFalso) FuncionID(_ context.Context, ref string) (int, error) {
	if ref == "f1" || ref == "1" {
		return 1, nil
	}
	return 0, domain.ErrNoExiste
}
func (c *catalogoFalso) CodigoFuncion(context.Context, int) (string, error)      { return "f1", nil }
func (c *catalogoFalso) AsientosVendidos(context.Context, int) ([]string, error) { return nil, nil }
func (c *catalogoFalso) FueraServicio(context.Context, int) ([]string, error)    { return nil, nil }
func (c *catalogoFalso) VendidosVigentes(context.Context) (map[int]string, map[int]domain.Ocupacion, error) {
	return nil, nil, nil
}
func (c *catalogoFalso) ContextoReserva(_ context.Context, _ int, asientos []string, _ int64) (*string, int, int, error) {
	if c.iniciada {
		return nil, 0, 0, domain.ErrFuncionIniciada
	}
	vendidos := 0
	for _, a := range asientos {
		if c.filas > 0 && int(a[0]-'A') >= c.filas {
			return nil, 0, 0, domain.ErrAsientoInexistente
		}
		if c.vendidos[a] {
			vendidos++
		}
	}
	return c.evento, vendidos, 0, nil
}

// candadosFalsos guarda las butacas retenidas y las reservas en mapas.
type candadosFalsos struct {
	retenidas map[string]string // butaca → reserva
	reservas  map[string]domain.Reserva
}

func nuevosCandados() *candadosFalsos {
	return &candadosFalsos{retenidas: map[string]string{}, reservas: map[string]domain.Reserva{}}
}
func (k *candadosFalsos) Bloquear(context.Context, int, string, int64, time.Duration, int) (int, error) {
	return 1, nil
}
func (k *candadosFalsos) LiberarBloqueos(context.Context, int, []string, int64) ([]string, error) {
	return nil, nil
}
func (k *candadosFalsos) Retener(_ context.Context, r domain.Reserva, _ time.Duration) (string, error) {
	for _, a := range r.Asientos {
		if dueño, ok := k.retenidas[a]; ok && dueño != r.ID {
			return a, nil
		}
	}
	for _, a := range r.Asientos {
		k.retenidas[a] = r.ID
	}
	return "", nil
}
func (k *candadosFalsos) Retenidos(context.Context, int) []string              { return nil }
func (k *candadosFalsos) RetenidosPorFuncion(context.Context) map[int][]string { return nil }
func (k *candadosFalsos) BloqueosDe(context.Context, int, int64) []string      { return nil }
func (k *candadosFalsos) TotalRetenidos(context.Context) int                   { return len(k.retenidas) }
func (k *candadosFalsos) GuardarReserva(_ context.Context, r domain.Reserva, _ time.Duration) error {
	k.reservas[r.ID] = r
	return nil
}
func (k *candadosFalsos) Reserva(_ context.Context, id string) (domain.Reserva, error) {
	r, ok := k.reservas[id]
	if !ok {
		return r, domain.ErrNoExiste
	}
	return r, nil
}
func (k *candadosFalsos) BorrarReserva(_ context.Context, r domain.Reserva) ([]string, error) {
	delete(k.reservas, r.ID)
	for _, a := range r.Asientos {
		delete(k.retenidas, a)
	}
	return r.Asientos, nil
}
func (k *candadosFalsos) Publicar(context.Context, int, domain.EventoAsientos) error { return nil }
func (k *candadosFalsos) Suscribir(context.Context, int) (<-chan string, func(), error) {
	return nil, func() {}, nil
}
func (k *candadosFalsos) Expiraciones(context.Context, func(funcionID int, asiento string)) {}

// ventasFalsas registra cuántas veces se cobró y devuelve los boletos de cada reserva pagada.
type ventasFalsas struct {
	cobros    int
	validados int
	boletos   map[string][]domain.Boleto
	saldo     int
}

func (v *ventasFalsas) BoletosDeReserva(_ context.Context, id string) []domain.Boleto {
	return v.boletos[id]
}
func (v *ventasFalsas) RegistrarVenta(_ context.Context, r domain.Reserva) (int, error) {
	if v.saldo < 60*len(r.Asientos) {
		return 0, domain.ErrSaldoInsuficiente
	}
	v.cobros++
	v.saldo -= 60 * len(r.Asientos)
	for _, a := range r.Asientos {
		v.boletos[r.ID] = append(v.boletos[r.ID], domain.Boleto{Token: "CE-" + a, Funcion: "f1", Asiento: a, Precio: 60, Estado: "valido"})
	}
	return v.saldo, nil
}
func (v *ventasFalsas) ValidarBoleto(context.Context, string) (domain.Validacion, error) {
	v.validados++
	return domain.Validacion{OK: true, Motivo: "Acceso permitido"}, nil
}

// turnosFalsos concede turno sólo a los usuarios de la lista.
type turnosFalsos map[int64]bool

func (t turnosFalsos) ConfirmarTurno(context.Context, *string, int64) {}

func (t turnosFalsos) ExigirTurno(_ context.Context, evento *string, usuarioID int64) error {
	if evento != nil && !t[usuarioID] {
		return domain.Prohibido("sin turno")
	}
	return nil
}

func armar(cat *catalogoFalso, turnos turnosFalsos, saldo int) (*Reservas, *candadosFalsos, *ventasFalsas) {
	k := nuevosCandados()
	v := &ventasFalsas{boletos: map[string][]domain.Boleto{}, saldo: saldo}
	a := NuevoAsientos(cat, k, turnos, 5*time.Minute)
	return NuevoReservas(a, cat, k, v, 5*time.Minute), k, v
}

func estado(err error) int {
	var e *domain.Error
	if errors.As(err, &e) {
		return e.Estado
	}
	return 0
}

func TestCrearReservaRetieneLasButacas(t *testing.T) {
	r, k, _ := armar(&catalogoFalso{}, nil, 120)
	id, expira, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"C-04", "C-05"}})
	if err != nil {
		t.Fatalf("la reserva debería crearse: %v", err)
	}
	if k.retenidas["C-04"] != id || k.retenidas["C-05"] != id {
		t.Fatalf("las butacas deberían quedar retenidas por la reserva %s: %v", id, k.retenidas)
	}
	if time.Until(expira) < 4*time.Minute {
		t.Fatalf("la retención debería durar 5 min, vence %v", expira)
	}
}

func TestCrearReservaRechazaCasosInvalidos(t *testing.T) {
	evento := "e1"
	casos := []struct {
		nombre string
		cat    *catalogoFalso
		sol    SolicitudReserva
		estado int
	}{
		{"función inexistente", &catalogoFalso{}, SolicitudReserva{Funcion: "f99", UsuarioID: 7, Asientos: []string{"A-01"}}, http.StatusNotFound},
		{"función ya iniciada", &catalogoFalso{iniciada: true}, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01"}}, http.StatusGone},
		{"butaca vendida", &catalogoFalso{vendidos: map[string]bool{"A-01": true}}, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01"}}, http.StatusConflict},
		{"preestreno sin turno", &catalogoFalso{evento: &evento}, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01"}}, http.StatusForbidden},
		{"butaca con formato inválido", &catalogoFalso{}, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"Z-1"}}, http.StatusBadRequest},
		{"demasiadas butacas", &catalogoFalso{}, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01", "A-02", "A-03", "A-04", "A-05", "A-06", "A-07"}}, http.StatusBadRequest},
	}
	for _, c := range casos {
		r, _, _ := armar(c.cat, turnosFalsos{}, 120)
		_, _, err := r.Crear(context.Background(), c.sol)
		if estado(err) != c.estado {
			t.Errorf("%s: se esperaba %d, fue %v", c.nombre, c.estado, err)
		}
	}
}

func TestPreestrenoConTurnoSePuedeReservar(t *testing.T) {
	evento := "e1"
	r, _, _ := armar(&catalogoFalso{evento: &evento}, turnosFalsos{7: true}, 120)
	if _, _, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01"}}); err != nil {
		t.Fatalf("con turno vigente la reserva del preestreno debería crearse: %v", err)
	}
}

func TestDosClientesNoRetienenLaMismaButaca(t *testing.T) {
	r, _, _ := armar(&catalogoFalso{}, nil, 120)
	ctx := context.Background()
	if _, _, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"B-02"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 8, Asientos: []string{"B-02"}})
	if estado(err) != http.StatusConflict {
		t.Fatalf("el segundo cliente debería recibir 409, fue %v", err)
	}
}

func TestPagarEsIdempotente(t *testing.T) {
	r, k, v := armar(&catalogoFalso{}, nil, 120)
	ctx := context.Background()
	id, _, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"D-01", "D-02"}})
	if err != nil {
		t.Fatal(err)
	}
	primero, codigo, err := r.Pagar(ctx, id)
	if err != nil || codigo != http.StatusCreated || len(primero.Boletos) != 2 {
		t.Fatalf("el pago debería emitir 2 boletos con 201: %v %d %+v", err, codigo, primero)
	}
	if len(k.retenidas) != 0 {
		t.Fatalf("al pagar se liberan los candados: %v", k.retenidas)
	}
	repetido, codigo, err := r.Pagar(ctx, id)
	if err != nil || codigo != http.StatusOK || !repetido.Repetido || len(repetido.Boletos) != 2 {
		t.Fatalf("repetir el pago debería devolver los mismos boletos con 200: %v %d %+v", err, codigo, repetido)
	}
	if v.cobros != 1 {
		t.Fatalf("se cobró %d veces; un pago repetido no debe volver a cobrar", v.cobros)
	}
}

func TestPagarSinSaldoNoEmiteBoletos(t *testing.T) {
	r, _, v := armar(&catalogoFalso{}, nil, 50)
	ctx := context.Background()
	id, _, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"E-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Pagar(ctx, id); estado(err) != http.StatusPaymentRequired {
		t.Fatalf("sin saldo se esperaba 402, fue %v", err)
	}
	if v.cobros != 0 || len(v.boletos[id]) != 0 {
		t.Fatal("sin saldo no se debe cobrar ni emitir boletos")
	}
}

func TestPagarReservaVencida(t *testing.T) {
	r, _, _ := armar(&catalogoFalso{}, nil, 120)
	if _, _, err := r.Pagar(context.Background(), "R-INEXISTENTE"); estado(err) != http.StatusGone {
		t.Fatalf("una reserva vencida debería responder 410, fue %v", err)
	}
}

func TestNoSeVendenButacasQueNoExistenEnLaSala(t *testing.T) {
	r, k, _ := armar(&catalogoFalso{filas: 6}, nil, 120)
	if _, _, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"F-01", "J-12"}}); estado(err) != 400 {
		t.Fatalf("J-12 no existe en una sala de 6 filas: debe ser 400, fue %v", err)
	}
	if len(k.retenidas) != 0 {
		t.Fatalf("no debe retenerse ninguna butaca: %v", k.retenidas)
	}
	if err := r.asientos.Bloquear(context.Background(), "f1", "G-01", 7); estado(err) != 400 {
		t.Fatalf("tampoco se puede bloquear desde el mapa: %v", err)
	}
}
