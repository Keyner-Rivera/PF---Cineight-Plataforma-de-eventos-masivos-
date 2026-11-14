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
	cobros      int
	validados   int
	boletos     map[string][]domain.Boleto
	duenos      map[string]int64 // reserva → usuario que la pagó
	retenciones *retencionesFalsas
	saldo       int
}

func (v *ventasFalsas) BoletoDe(context.Context, string, int64) (string, error) { return "valido", nil }
func (v *ventasFalsas) BoletosDeReserva(_ context.Context, id string, usuarioID int64) []domain.Boleto {
	if v.duenos[id] != usuarioID { // como el filtro b.usuario_id = $2 de la consulta real
		return nil
	}
	return v.boletos[id]
}
func (v *ventasFalsas) RegistrarVenta(_ context.Context, r domain.Reserva, _ int) (int, int, error) {
	if v.retenciones != nil && !v.retenciones.vigente(r) {
		return 0, 0, domain.ErrReservaVencida
	}
	if v.saldo < 60*len(r.Asientos) {
		return 0, 0, domain.ErrSaldoInsuficiente
	}
	v.cobros++
	v.saldo -= 60 * len(r.Asientos)
	if v.duenos == nil {
		v.duenos = map[string]int64{}
	}
	v.duenos[r.ID] = r.UsuarioID
	for _, a := range r.Asientos {
		v.boletos[r.ID] = append(v.boletos[r.ID], domain.Boleto{Token: "CE-" + a, Funcion: "f1", Asiento: a, Precio: 60, Estado: "valido", Reserva: r.ID})
	}
	if v.retenciones != nil {
		_ = v.retenciones.LiberarReserva(context.Background(), r.ID, r.UsuarioID)
	}
	return v.saldo, 60 * len(r.Asientos), nil
}
func (v *ventasFalsas) Comprobante(_ context.Context, folio string, usuario int64) (domain.Comprobante, error) {
	bs := v.boletos[folio]
	if len(bs) == 0 || (usuario != 0 && v.duenos[folio] != usuario) {
		return domain.Comprobante{}, domain.ErrNoExiste
	}
	c := domain.Comprobante{Folio: folio, Fecha: time.Unix(1_800_000_000, 0), Cliente: "Ana López", Correo: "ana@correo.com", Funcion: "f1", PrecioUnitario: 60, Boletos: bs}
	for _, b := range bs {
		c.Asientos = append(c.Asientos, b.Asiento)
		c.Total += b.Precio
	}
	return c, nil
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
	rt := nuevasRetenciones()
	v := &ventasFalsas{boletos: map[string][]domain.Boleto{}, saldo: saldo, retenciones: rt}
	a := NuevoAsientos(cat, k, rt, turnos, 5*time.Minute)
	return NuevoReservas(a, cat, k, rt, v, 5*time.Minute), k, v
}

// retencionesFalsas se comporta como la tabla retenciones: una butaca tiene un solo dueño hasta que vence.
type retencionesFalsas struct {
	filas map[string]retencionFalsa // butaca → retención
	ahora func() time.Time
}

type retencionFalsa struct {
	usuario int64
	reserva string
	expira  time.Time
}

func nuevasRetenciones() *retencionesFalsas {
	return &retencionesFalsas{filas: map[string]retencionFalsa{}, ahora: time.Now}
}

func (r *retencionesFalsas) tomar(asiento string, usuario int64, reserva string, ttl time.Duration) bool {
	if f, ok := r.filas[asiento]; ok && r.ahora().Before(f.expira) && !(f.usuario == usuario && f.reserva == "") {
		return false
	}
	r.filas[asiento] = retencionFalsa{usuario, reserva, r.ahora().Add(ttl)}
	return true
}
func (r *retencionesFalsas) RetenerSeleccion(_ context.Context, _ int, asiento string, usuario int64, ttl time.Duration, _ int) error {
	if !r.tomar(asiento, usuario, "", ttl) {
		return domain.ErrButacaOcupada
	}
	return nil
}
func (r *retencionesFalsas) SoltarSeleccion(_ context.Context, _ int, asientos []string, usuario int64) error {
	for _, a := range asientos {
		if f := r.filas[a]; f.usuario == usuario && f.reserva == "" {
			delete(r.filas, a)
		}
	}
	return nil
}
func (r *retencionesFalsas) RetenerReserva(_ context.Context, res domain.Reserva, ttl time.Duration, _ int) (int, error) {
	previas := map[string]retencionFalsa{}
	for _, a := range res.Asientos {
		previas[a] = r.filas[a]
		if !r.tomar(a, res.UsuarioID, res.ID, ttl) {
			for b, f := range previas { // todo o nada, como la transacción
				if f.expira.IsZero() {
					delete(r.filas, b)
				} else {
					r.filas[b] = f
				}
			}
			return 0, domain.ErrButacaOcupada
		}
	}
	return 60, nil
}
func (r *retencionesFalsas) LiberarReserva(_ context.Context, reserva string, usuario int64) error {
	for a, f := range r.filas {
		if f.reserva == reserva && f.usuario == usuario {
			delete(r.filas, a)
		}
	}
	return nil
}
func (r *retencionesFalsas) LimpiarRetencionesVencidas(context.Context) (map[int][]string, error) {
	out := map[int][]string{}
	for a, f := range r.filas {
		if !r.ahora().Before(f.expira) {
			delete(r.filas, a)
			out[1] = append(out[1], a)
		}
	}
	return out, nil
}

// vigente indica si la reserva sigue retenida en la "base" (lo que RegistrarVenta exige dentro de su transacción).
func (r *retencionesFalsas) vigente(res domain.Reserva) bool {
	for _, a := range res.Asientos {
		f, ok := r.filas[a]
		if !ok || f.reserva != res.ID || f.usuario != res.UsuarioID || !r.ahora().Before(f.expira) {
			return false
		}
	}
	return true
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
	res, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"C-04", "C-05"}})
	if err != nil {
		t.Fatalf("la reserva debería crearse: %v", err)
	}
	id, expira := res.ID, res.Expira
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
		_, err := r.Crear(context.Background(), c.sol)
		if estado(err) != c.estado {
			t.Errorf("%s: se esperaba %d, fue %v", c.nombre, c.estado, err)
		}
	}
}

func TestPreestrenoConTurnoSePuedeReservar(t *testing.T) {
	evento := "e1"
	r, _, _ := armar(&catalogoFalso{evento: &evento}, turnosFalsos{7: true}, 120)
	if _, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"A-01"}}); err != nil {
		t.Fatalf("con turno vigente la reserva del preestreno debería crearse: %v", err)
	}
}

func TestDosClientesNoRetienenLaMismaButaca(t *testing.T) {
	r, _, _ := armar(&catalogoFalso{}, nil, 120)
	ctx := context.Background()
	if _, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"B-02"}}); err != nil {
		t.Fatal(err)
	}
	_, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 8, Asientos: []string{"B-02"}})
	if estado(err) != http.StatusConflict {
		t.Fatalf("el segundo cliente debería recibir 409, fue %v", err)
	}
}

func TestPagarEsIdempotente(t *testing.T) {
	r, k, v := armar(&catalogoFalso{}, nil, 120)
	ctx := context.Background()
	res, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"D-01", "D-02"}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.ID
	primero, codigo, err := r.Pagar(ctx, id, 7)
	if err != nil || codigo != http.StatusCreated || len(primero.Boletos) != 2 {
		t.Fatalf("el pago debería emitir 2 boletos con 201: %v %d %+v", err, codigo, primero)
	}
	if len(k.retenidas) != 0 {
		t.Fatalf("al pagar se liberan los candados: %v", k.retenidas)
	}
	repetido, codigo, err := r.Pagar(ctx, id, 7)
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
	res, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"E-01"}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.ID
	if _, _, err := r.Pagar(ctx, id, 7); estado(err) != http.StatusPaymentRequired {
		t.Fatalf("sin saldo se esperaba 402, fue %v", err)
	}
	if v.cobros != 0 || len(v.boletos[id]) != 0 {
		t.Fatal("sin saldo no se debe cobrar ni emitir boletos")
	}
}

func TestPagarReservaVencida(t *testing.T) {
	r, _, _ := armar(&catalogoFalso{}, nil, 120)
	if _, _, err := r.Pagar(context.Background(), "R-INEXISTENTE", 7); estado(err) != http.StatusGone {
		t.Fatalf("una reserva vencida debería responder 410, fue %v", err)
	}
}

func TestNoSeVendenButacasQueNoExistenEnLaSala(t *testing.T) {
	r, k, _ := armar(&catalogoFalso{filas: 6}, nil, 120)
	if _, err := r.Crear(context.Background(), SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"F-01", "J-12"}}); estado(err) != 400 {
		t.Fatalf("J-12 no existe en una sala de 6 filas: debe ser 400, fue %v", err)
	}
	if len(k.retenidas) != 0 {
		t.Fatalf("no debe retenerse ninguna butaca: %v", k.retenidas)
	}
	if err := r.asientos.Bloquear(context.Background(), "f1", "G-01", 7); estado(err) != 400 {
		t.Fatalf("tampoco se puede bloquear desde el mapa: %v", err)
	}
}

func TestNadiePagaNiCancelaUnaReservaAjena(t *testing.T) {
	r, k, v := armar(&catalogoFalso{}, nil, 120)
	ctx := context.Background()
	res, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"F-01"}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.ID
	if _, _, err := r.Pagar(ctx, id, 8); estado(err) != http.StatusNotFound || v.cobros != 0 {
		t.Fatalf("otro usuario no puede pagar la reserva (404 sin cobrar): %v, cobros %d", err, v.cobros)
	}
	if err := r.Cancelar(ctx, id, 8); estado(err) != http.StatusNotFound || len(k.retenidas) != 1 {
		t.Fatalf("otro usuario no puede cancelarla: %v, retenidas %v", err, k.retenidas)
	}
	if _, _, err := r.Pagar(ctx, id, 7); err != nil {
		t.Fatalf("el dueño sí paga: %v", err)
	}
	if p, _, err := r.Pagar(ctx, id, 8); err == nil && len(p.Boletos) > 0 {
		t.Fatal("repetir el pago ajeno no debe revelar los boletos del dueño")
	}
}
