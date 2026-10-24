package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/pkg/recaptcha"
)

// ───────── Cola virtual (RF-36, RF-51) ─────────

// ColaVirtual atiende a los clientes de un preestreno por orden de llegada: como mucho `capacidad` clientes
// eligen butacas a la vez. La cola conoce el inventario del preestreno:
//   - no da más turnos que los que alcanzan las butacas sin tomar (2 por cliente como máximo),
//   - avisa a quien espera si su lugar alcanza butaca (pronóstico) y cuántas quedan,
//   - se cierra con "agotado" cuando ya no queda ninguna y no admite a quien ya tiene sus boletos.
//
// Cada turno empieza con un minuto de gracia; si el cliente bloquea una butaca se extiende a la duración completa.
// Quien espera debe consultar su posición periódicamente; si deja de hacerlo durante `inactividad`, pierde su lugar.
type ColaVirtual struct {
	colas       Colas
	guardia     GuardiaAntiBots
	eventos     Eventos
	candados    Candados
	capacidad   int64
	turno       time.Duration
	gracia      time.Duration
	inactividad time.Duration

	mu         sync.Mutex
	inventario map[string]inventarioCache // por evento: lo consultan cientos de clientes cada pocos segundos
}

type inventarioCache struct {
	inv   domain.InventarioEvento
	vence time.Time
}

// vidaInventario es cuánto se reutiliza el inventario del preestreno entre consultas de la cola.
const vidaInventario = time.Second

// NuevaCola crea el servicio. capacidad = clientes atendidos a la vez; turno = tiempo para elegir butacas y pagar.
func NuevaCola(c Colas, g GuardiaAntiBots, e Eventos, k Candados, capacidad int64, turno time.Duration) *ColaVirtual {
	return &ColaVirtual{
		colas: c, guardia: g, eventos: e, candados: k,
		capacidad: max(capacidad, 1), turno: turno, gracia: min(time.Minute, turno),
		// 2 minutos: los navegadores frenan los temporizadores de las pestañas en segundo plano (hasta 1 por minuto),
		// así quien cambia de pestaña mientras espera no pierde su lugar.
		inactividad: 2 * time.Minute,
		inventario:  map[string]inventarioCache{},
	}
}

// formatoEvento valida identificadores como "e1".
var formatoEvento = regexp.MustCompile(`^e[0-9]{1,6}$`)

func (s *ColaVirtual) validar(evento string, usuarioID int64) error {
	if !formatoEvento.MatchString(evento) {
		return domain.NoEncontrado("El preestreno no existe.")
	}
	if usuarioID <= 0 {
		return domain.Invalido("usuario_id requerido")
	}
	return nil
}

// inventarioDe devuelve las butacas del preestreno, reutilizando la consulta durante vidaInventario.
func (s *ColaVirtual) inventarioDe(ctx context.Context, evento string) (domain.InventarioEvento, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.inventario[evento]; ok && time.Now().Before(c.vence) {
		return c.inv, nil
	}
	inv, err := s.eventos.InventarioEvento(ctx, evento)
	if errors.Is(err, domain.ErrNoExiste) {
		return inv, domain.NoEncontrado("No hay un preestreno por venir.")
	}
	if err != nil {
		return inv, err
	}
	inv.Retenidas = len(s.candados.Retenidos(ctx, inv.FuncionID))
	s.inventario[evento] = inventarioCache{inv: inv, vence: time.Now().Add(vidaInventario)}
	return inv, nil
}

// cerrada responde la posición de una cola que ya no atiende (agotada o con la función iniciada), o nil si sigue abierta.
func cerrada(inv domain.InventarioEvento, capacidad int64) *domain.PosicionCola {
	switch {
	case !inv.Abierta:
		return &domain.PosicionCola{Cerrada: true, Capacidad: capacidad}
	case inv.Libres <= 0:
		return &domain.PosicionCola{Agotado: true, Capacidad: capacidad}
	}
	return nil
}

// Entrar verifica que el preestreno siga a la venta y que el cliente no tenga ya sus boletos, exige el pase
// anti-bots, agrega al usuario a la fila y devuelve su posición.
func (s *ColaVirtual) Entrar(ctx context.Context, evento string, usuarioID int64, pase string) (domain.PosicionCola, error) {
	if err := s.validar(evento, usuarioID); err != nil {
		return domain.PosicionCola{}, err
	}
	// Antes de canjear el pase (es de un solo uso): si no puede comprar, no tiene sentido gastarlo.
	inv, err := s.inventarioDe(ctx, evento)
	if err != nil {
		return domain.PosicionCola{}, err
	}
	if p := cerrada(inv, s.capacidad); p != nil {
		return *p, nil
	}
	propios, err := s.eventos.BoletosDeUsuario(ctx, inv.FuncionID, usuarioID)
	if err != nil {
		return domain.PosicionCola{}, err
	}
	if propios >= domain.MaxBoletosPorEvento {
		return domain.PosicionCola{}, domain.Conflicto(fmt.Sprintf("Ya tienes los %d boletos permitidos para este preestreno.", domain.MaxBoletosPorEvento))
	}
	if err := s.guardia.ExigirPase(ctx, pase, "cola virtual"); err != nil {
		return domain.PosicionCola{}, err
	}
	if err := s.colas.EntrarCola(ctx, evento, strconv.FormatInt(usuarioID, 10), time.Now()); err != nil {
		return domain.PosicionCola{}, err
	}
	return s.Posicion(ctx, evento, usuarioID)
}

// cupoTurnos es cuántos clientes pueden tener turno a la vez: la capacidad configurada, pero no más de los que
// alcanzan las butacas sin tomar si cada uno se lleva el máximo permitido.
func cupoTurnos(capacidad int64, disponibles int) int64 {
	alcanzan := int64((disponibles + domain.MaxBoletosPorEvento - 1) / domain.MaxBoletosPorEvento)
	return min(capacidad, alcanzan)
}

// pronosticar estima si quien está en la posición indicada alcanzará butaca: "alcanza" si quedan butacas aunque
// todos los de adelante se lleven el máximo, "no-alcanza" si no quedarían ni aunque cada uno compre una sola, e
// "incierto" en medio (depende de cuántas compren los de adelante).
func pronosticar(disponibles int, posicion int64) string {
	adelante := int(posicion - 1)
	switch {
	case disponibles-adelante*domain.MaxBoletosPorEvento >= 1:
		return "alcanza"
	case disponibles-adelante < 1:
		return "no-alcanza"
	}
	return "incierto"
}

// Posicion avanza la cola (admite a los siguientes si hay cupo y butacas) y devuelve el lugar o el turno del usuario.
// Cada consulta cuenta como señal de que el usuario sigue esperando.
func (s *ColaVirtual) Posicion(ctx context.Context, evento string, usuarioID int64) (domain.PosicionCola, error) {
	if err := s.validar(evento, usuarioID); err != nil {
		return domain.PosicionCola{}, err
	}
	inv, err := s.inventarioDe(ctx, evento)
	if err != nil {
		return domain.PosicionCola{}, err
	}
	if p := cerrada(inv, s.capacidad); p != nil {
		return *p, nil
	}
	disponibles := inv.Disponibles()
	e, err := s.colas.AvanzarCola(ctx, evento, strconv.FormatInt(usuarioID, 10), time.Now(), cupoTurnos(s.capacidad, disponibles), s.gracia, s.inactividad)
	if err != nil {
		return domain.PosicionCola{}, err
	}
	p := domain.PosicionCola{EnCola: e.EnCola, Capacidad: s.capacidad, ButacasLibres: disponibles}
	switch {
	case e.Turno:
		p.TurnoHabilitado = true
		p.Expira = &e.Vence
		p.Confirmado = e.Confirmado
	case e.EnFila:
		p.Posicion = e.Posicion
		p.Pronostico = pronosticar(disponibles, e.Posicion)
		// Peor caso: entran `capacidad` clientes por cada turno completo que transcurre.
		p.EsperaMin = int((e.Posicion+s.capacidad-1)/s.capacidad) * int(s.turno.Minutes())
	default:
		return domain.PosicionCola{}, domain.NoEncontrado("No estás en la cola. Vuelve a ingresar.")
	}
	return p, nil
}

// Salir libera el lugar o el turno del usuario (abandonó la cola o ya compró sus boletos).
func (s *ColaVirtual) Salir(ctx context.Context, evento string, usuarioID int64) error {
	if err := s.validar(evento, usuarioID); err != nil {
		return err
	}
	return s.colas.SalirCola(ctx, evento, strconv.FormatInt(usuarioID, 10))
}

// ConfirmarTurno extiende el turno a su duración completa cuando el cliente bloquea o reserva butacas del
// preestreno dentro del minuto de gracia. Si falla, el cliente conserva el turno de gracia: no se interrumpe la compra.
func (s *ColaVirtual) ConfirmarTurno(ctx context.Context, evento *string, usuarioID int64) {
	if evento == nil {
		return
	}
	if err := s.colas.ConfirmarTurno(ctx, *evento, strconv.FormatInt(usuarioID, 10), time.Now(), s.turno); err != nil {
		log.Printf("cola virtual: no se pudo confirmar el turno de %d en %s: %v", usuarioID, *evento, err)
	}
}

// ExigirTurno impide bloquear o reservar butacas de un preestreno sin turno vigente (RF-36, RF-51).
// Las funciones normales (evento nil) no lo requieren.
func (s *ColaVirtual) ExigirTurno(ctx context.Context, evento *string, usuarioID int64) error {
	if evento == nil {
		return nil
	}
	ok, err := s.colas.TurnoActivo(ctx, *evento, strconv.FormatInt(usuarioID, 10), time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return domain.Prohibido("Este preestreno se vende por cola virtual: necesitas un turno vigente para elegir butacas.")
	}
	return nil
}

// ───────── Métricas (RF-56) ─────────

// Panel calcula los indicadores del panel de administración.
type Panel struct {
	cuentas  Cuentas
	candados Candados
	colas    Colas
}

// NuevoPanel crea el servicio.
func NuevoPanel(c Cuentas, k Candados, q Colas) *Panel {
	return &Panel{cuentas: c, candados: k, colas: q}
}

// Metricas combina PostgreSQL (ventas, créditos) y Redis (retenciones, colas).
func (s *Panel) Metricas(ctx context.Context) (domain.Metricas, error) {
	m, err := s.cuentas.Metricas(ctx)
	m.AsientosRetenidos = s.candados.TotalRetenidos(ctx)
	m.Colas = s.colas.Colas(ctx)
	return m, err
}

// HistorialClientes devuelve las compras de cada cliente agrupadas por película (estadísticas de clientes).
func (s *Panel) HistorialClientes(ctx context.Context) ([]domain.HistorialCliente, error) {
	return s.cuentas.HistorialClientes(ctx)
}

// ───────── Seguridad (RF-53) ─────────

// Seguridad agrupa el anti-bots: reCAPTCHA v3, pases de un solo uso y limitación de solicitudes por IP.
type Seguridad struct {
	captcha   *recaptcha.Verificador
	colas     Colas
	pases     Pases
	auditoria Auditoria
}

// NuevaSeguridad crea el servicio.
func NuevaSeguridad(v *recaptcha.Verificador, c Colas, p Pases, a Auditoria) *Seguridad {
	return &Seguridad{captcha: v, colas: c, pases: p, auditoria: a}
}

// VigenciaPase es lo que dura el pase emitido tras superar reCAPTCHA.
const VigenciaPase = 10 * time.Minute

// AccionesCaptcha son las acciones que el frontend puede declarar al pedir un token.
var AccionesCaptcha = map[string]bool{"login": true, "registro": true, "cola": true}

// VerificarCaptcha confirma un token de reCAPTCHA v3. Los rechazos quedan en la bitácora.
// Si el token es válido devuelve también un pase de un solo uso: los tokens de Google no se pueden verificar
// dos veces, así que la solicitud que crea la cuenta presenta el pase en lugar del token.
func (s *Seguridad) VerificarCaptcha(ctx context.Context, tok, accion, ip string) (r recaptcha.Resultado, pase string, err error) {
	if !AccionesCaptcha[accion] {
		return recaptcha.Resultado{}, "", domain.Invalido("Acción de verificación desconocida.")
	}
	r, err = s.captcha.Verificar(ctx, tok, accion, ip)
	if err != nil {
		return r, "", err
	}
	if !r.Valido {
		s.auditoria.Auditar(ctx, nil, "CAPTCHA_RECHAZADO", "seguridad", map[string]any{"ip": ip, "accion": accion, "motivo": r.Motivo, "puntaje": r.Puntaje})
		return r, "", nil
	}
	if r.Activo {
		pase, err = s.pases.EmitirPase(ctx, VigenciaPase)
	}
	return r, pase, err
}

// ExigirPase consume el pase anti-bots. Sin clave secreta de reCAPTCHA (entorno local) no exige nada.
func (s *Seguridad) ExigirPase(ctx context.Context, pase, motivo string) error {
	if !s.captcha.Activo() {
		return nil
	}
	ok, err := s.pases.CanjearPase(ctx, pase)
	if err != nil {
		return err
	}
	if !ok {
		s.auditoria.Auditar(ctx, nil, "PASE_ANTIBOTS_AUSENTE", "seguridad", map[string]any{"motivo": motivo})
		return domain.Prohibido("Verificación anti-bots requerida. Vuelve a iniciar sesión.")
	}
	return nil
}

// Permitir cuenta la solicitud de una IP en un grupo de rutas y dice si sigue bajo el límite por minuto.
// Si Redis falla, la solicitud se permite (fail-open): el límite protege, pero no debe tumbar la venta.
func (s *Seguridad) Permitir(ctx context.Context, grupo, ip, ruta string, limite int64) bool {
	n, err := s.colas.Contar(ctx, grupo, ip)
	if err != nil || n <= limite {
		return true
	}
	if n == limite+1 { // sólo el primer bloqueo del minuto va a la bitácora
		s.auditoria.Auditar(ctx, nil, "SOLICITUD_BLOQUEADA", "rate_limit", map[string]any{"ip": ip, "ruta": ruta})
	}
	return false
}
