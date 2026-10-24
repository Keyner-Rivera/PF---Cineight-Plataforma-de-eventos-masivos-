// Package services contiene la lógica de negocio. Cada servicio depende de interfaces (puertos)
// y no de implementaciones concretas, así que puede probarse con dobles en memoria.
package services

import (
	"context"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// Catalogo resuelve funciones y su ocupación vendida.
type Catalogo interface {
	Funciones(ctx context.Context) ([]domain.Funcion, error)
	FuncionID(ctx context.Context, ref string) (int, error)
	CodigoFuncion(ctx context.Context, id int) (string, error)
	AsientosVendidos(ctx context.Context, funcionID int) ([]string, error)
	FueraServicio(ctx context.Context, funcionID int) ([]string, error)
	VendidosVigentes(ctx context.Context) (codigos map[int]string, ocupacion map[int]domain.Ocupacion, err error)
	ContextoReserva(ctx context.Context, funcionID int, asientos []string, usuarioID int64) (evento *string, vendidos, propios int, err error)
}

// Ventas persiste pagos y validaciones de boletos.
type Ventas interface {
	BoletosDeReserva(ctx context.Context, reservaID string) []domain.Boleto
	RegistrarVenta(ctx context.Context, r domain.Reserva) (saldoFinal int, err error)
	ValidarBoleto(ctx context.Context, token string) (domain.Validacion, error)
}

// Cuentas gestiona clientes y créditos.
type Cuentas interface {
	ExisteCliente(ctx context.Context, correo string) (bool, error)
	VincularCliente(ctx context.Context, correo, nombre string) (id int64, saldo int, err error)
	Cuenta(ctx context.Context, usuarioID int64) (domain.Cuenta, error)
	AcreditarTrivia(ctx context.Context, usuarioID int64, partida string, monto int) (acreditado bool, saldo int, err error)
	Metricas(ctx context.Context) (domain.Metricas, error)
	HistorialClientes(ctx context.Context) ([]domain.HistorialCliente, error)
}

// PlanificadorDias completa la cartelera de los días indicados según el plan de salas.
// Migrar aplica antes los cambios de esquema y de datos pendientes.
type PlanificadorDias interface {
	Migrar(ctx context.Context) error
	ProgramarDias(ctx context.Context, dias []time.Time, plan []domain.PlanSala) (int, error)
}

// Mantenimiento administra las butacas que el personal retira de la venta.
type Mantenimiento interface {
	ButacasFueraServicio(ctx context.Context, sala string) ([]domain.ButacaFueraServicio, error)
	MarcarFueraServicio(ctx context.Context, sala, asiento, motivo, por string) error
	HabilitarButaca(ctx context.Context, sala, asiento string) error
	FuncionesVigentesDeSala(ctx context.Context, sala string) (map[int]string, error)
}

// Pases son comprobantes de un solo uso de que el cliente superó reCAPTCHA.
type Pases interface {
	EmitirPase(ctx context.Context, ttl time.Duration) (string, error)
	CanjearPase(ctx context.Context, pase string) (bool, error)
}

// GuardiaAntiBots exige el pase anti-bots en las operaciones que crean recursos (p. ej. una cuenta con bono).
type GuardiaAntiBots interface {
	ExigirPase(ctx context.Context, pase, motivo string) error
}

// Auditoria registra operaciones críticas en la bitácora (RNF-22).
type Auditoria interface {
	Auditar(ctx context.Context, usuarioID *int64, accion, entidad string, valor any)
}

// Candados es el estado efímero de las butacas en Redis.
type Candados interface {
	Bloquear(ctx context.Context, funcionID int, asiento string, usuarioID int64, ttl time.Duration, maximo int) (int, error)
	LiberarBloqueos(ctx context.Context, funcionID int, asientos []string, usuarioID int64) ([]string, error)
	Retener(ctx context.Context, r domain.Reserva, ttl time.Duration) (string, error)
	Retenidos(ctx context.Context, funcionID int) []string
	RetenidosPorFuncion(ctx context.Context) map[int][]string
	BloqueosDe(ctx context.Context, funcionID int, usuarioID int64) []string
	TotalRetenidos(ctx context.Context) int
	GuardarReserva(ctx context.Context, r domain.Reserva, ttl time.Duration) error
	Reserva(ctx context.Context, id string) (domain.Reserva, error)
	BorrarReserva(ctx context.Context, r domain.Reserva) ([]string, error)
	Publicar(ctx context.Context, funcionID int, ev domain.EventoAsientos) error
	// Suscribir registra un stream de la función: canal de cambios (JSON) y función para darlo de baja.
	Suscribir(ctx context.Context, funcionID int) (<-chan string, func(), error)
	Expiraciones(ctx context.Context, fn func(funcionID int, asiento string))
}

// Colas es la cola virtual y el contador de solicitudes.
type Colas interface {
	EntrarCola(ctx context.Context, evento, usuario string, ahora time.Time) error
	AvanzarCola(ctx context.Context, evento, usuario string, ahora time.Time, cupo int64, gracia, inactividad time.Duration) (domain.EstadoCola, error)
	ConfirmarTurno(ctx context.Context, evento, usuario string, ahora time.Time, turno time.Duration) error
	SalirCola(ctx context.Context, evento, usuario string) error
	TurnoActivo(ctx context.Context, evento, usuario string, ahora time.Time) (bool, error)
	Colas(ctx context.Context) map[string]int64
	Contar(ctx context.Context, grupo, ip string) (int64, error)
}

// Cache guarda lecturas frecuentes con vencimiento. Si falla, el servicio consulta la base de datos.
type Cache interface {
	LeerCache(ctx context.Context, clave string) ([]byte, bool)
	GuardarCache(ctx context.Context, clave string, datos []byte, ttl time.Duration)
	BorrarCache(ctx context.Context, clave string)
}

// Turnos verifica que quien compra un preestreno tenga turno vigente de la cola virtual y, cuando bloquea
// butacas, confirma el turno para que dure completo.
type Turnos interface {
	ExigirTurno(ctx context.Context, evento *string, usuarioID int64) error
	ConfirmarTurno(ctx context.Context, evento *string, usuarioID int64)
}

// Eventos da a la cola virtual el inventario del preestreno y los boletos que ya tiene cada cliente.
type Eventos interface {
	InventarioEvento(ctx context.Context, evento string) (domain.InventarioEvento, error)
	BoletosDeUsuario(ctx context.Context, funcionID int, usuarioID int64) (int, error)
}
