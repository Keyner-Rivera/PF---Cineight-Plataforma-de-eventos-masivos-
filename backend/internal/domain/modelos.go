// Package domain define las entidades del negocio y las reglas fijas del prototipo.
// No depende de la base de datos ni de HTTP: las demás capas lo importan, nunca al revés.
package domain

import (
	"regexp"
	"time"
)

// Reglas de negocio (ERS, Tabla 31).
const (
	MaxAsientosPorCompra = 6   // RF-21
	MaxBoletosPorEvento  = 2   // RF-36: preestrenos de alta demanda
	BonoBienvenida       = 800 // créditos de una cuenta nueva
	CreditosPorAcierto   = 10
	BonoTriviaPerfecta   = 50
	PreguntasTrivia      = 3
)

// FormatoAsiento valida etiquetas como "C-04" (fila A–P, columna de dos dígitos).
var FormatoAsiento = regexp.MustCompile(`^[A-P]-\d{2}$`)

// Funcion es una proyección programada, tal como la consume el frontend.
type Funcion struct {
	ID          int       `json:"id"`
	Codigo      string    `json:"codigo"`
	Pelicula    string    `json:"pelicula"`
	Titulo      string    `json:"titulo"`
	Sala        string    `json:"sala"`
	SalaNombre  string    `json:"sala_nombre"`
	Inicio      time.Time `json:"inicio"`
	Fin         time.Time `json:"fin"`
	Formato     string    `json:"formato"`
	Idioma      string    `json:"idioma"`
	Precio      int       `json:"precio"`
	Evento      *string   `json:"evento"`
	Disponibles int       `json:"disponibles"`
}

// Categorías y clasificaciones válidas del catálogo.
var (
	Categorias      = map[string]bool{"cartelera": true, "estreno": true, "proximamente": true}
	Clasificaciones = map[string]bool{"G": true, "PG": true, "PG-13": true, "R": true, "NC-17": true}
)

// LimpiezaMin es el tiempo de limpieza entre funciones de una misma sala (RF-31).
const LimpiezaMin = 20

// Pelicula con su ficha completa (películas creadas desde el panel).
type Pelicula struct {
	Slug           string   `json:"slug"`
	Titulo         string   `json:"titulo"`
	TituloOriginal string   `json:"titulo_original"`
	Categoria      string   `json:"categoria"`
	Clasificacion  string   `json:"clasificacion"`
	Duracion       int      `json:"duracion"`
	Genero         string   `json:"genero"`
	Idioma         string   `json:"idioma"`
	Formatos       []string `json:"formatos"`
	Director       string   `json:"director"`
	Reparto        []string `json:"reparto"`
	Anio           string   `json:"anio"`
	Sinopsis       string   `json:"sinopsis"`
	Trailer        *string  `json:"trailer"`
	EstrenoEnDias  *int     `json:"estreno_en_dias"`
	PosterURL      string   `json:"poster_url"`
}

// PeliculaAlta es una película nueva ya validada, con su imagen decodificada.
type PeliculaAlta struct {
	Pelicula
	Poster     []byte
	PosterTipo string
	CreadaPor  string
}

// NuevaFuncion es una función a programar.
type NuevaFuncion struct {
	Sala    string    `json:"sala"` // código público (s1, s2…)
	Inicio  time.Time `json:"inicio"`
	Formato string    `json:"formato"`
	Idioma  string    `json:"idioma"`
	Precio  int       `json:"precio"`
}

// Ocupacion de una función: butacas vendidas y fuera de servicio (PostgreSQL) y retenidas o bloqueadas (Redis).
type Ocupacion struct {
	Vendidos      []string `json:"vendidos"`
	Retenidos     []string `json:"retenidos"`
	FueraServicio []string `json:"fuera_servicio"`
	Propios       []string `json:"propios,omitempty"` // bloqueos del usuario que consulta (?usuario=)
}

// Motivos por los que el personal retira una butaca de la venta.
var MotivosFueraServicio = map[string]bool{"danada": true, "fuera-servicio": true}

// ButacaFueraServicio es una butaca retirada de la venta en todas las funciones de su sala.
type ButacaFueraServicio struct {
	Asiento    string    `json:"asiento"`
	Motivo     string    `json:"motivo"`
	MarcadaPor string    `json:"marcada_por"`
	Desde      time.Time `json:"desde"`
}

// Boleto emitido tras un pago. Incluye los datos de su función para mostrarlo aunque la función ya
// haya terminado (GET /api/funciones sólo lista las vigentes).
type Boleto struct {
	Token    string    `json:"token"`
	Funcion  string    `json:"funcion"`
	Asiento  string    `json:"asiento"`
	Precio   int       `json:"precio"`
	Estado   string    `json:"estado"`
	Fecha    time.Time `json:"fecha"`
	Pelicula string    `json:"pelicula"`
	Sala     string    `json:"sala"`
	Inicio   time.Time `json:"inicio"`
	Formato  string    `json:"formato"`
	Idioma   string    `json:"idioma"`
}

// Movimiento de la cuenta de créditos.
type Movimiento struct {
	ID         int64     `json:"id"`
	Tipo       string    `json:"tipo"`
	Monto      int       `json:"monto"`
	Referencia string    `json:"referencia"`
	Fecha      time.Time `json:"fecha"`
}

// Cuenta de créditos con su historial.
type Cuenta struct {
	Saldo       int          `json:"saldo"`
	Movimientos []Movimiento `json:"movimientos"`
	Boletos     []Boleto     `json:"boletos"`
}

// Reserva temporal de butacas guardada en Redis mientras dura la retención.
type Reserva struct {
	ID        string
	FuncionID int
	UsuarioID int64
	Asientos  []string
}

// ResultadoPago de POST /api/reservas/{id}/pagar.
type ResultadoPago struct {
	ReservaID string   `json:"reserva_id"`
	Boletos   []Boleto `json:"boletos"`
	Saldo     *int     `json:"saldo,omitempty"`
	Repetido  bool     `json:"repetido,omitempty"`
}

// Validacion del acceso a sala.
type Validacion struct {
	OK      bool    `json:"ok"`
	Motivo  string  `json:"motivo"`
	Boleto  *Boleto `json:"boleto,omitempty"`
	Titular string  `json:"titular,omitempty"`
}

// InventarioEvento son las butacas de un preestreno que todavía se pueden vender.
type InventarioEvento struct {
	FuncionID int
	Abierta   bool // la función no ha comenzado
	Libres    int  // aforo menos vendidas y fuera de servicio (incluye las retenidas por otros clientes)
	Retenidas int  // bloqueadas o reservadas en este momento (Redis)
}

// Disponibles son las butacas que nadie tiene tomadas ahora mismo.
func (i InventarioEvento) Disponibles() int { return max(i.Libres-i.Retenidas, 0) }

// EstadoCola es la situación de un usuario en la cola virtual según Redis.
type EstadoCola struct {
	Turno      bool      // tiene turno vigente
	Confirmado bool      // bloqueó butacas dentro del minuto de gracia: el turno dura completo
	Vence      time.Time // fin del turno
	EnFila     bool      // está esperando
	Posicion   int64     // 1 = el siguiente en pasar
	EnCola     int64     // cuántos esperan en total
}

// PosicionCola en la cola virtual de un preestreno (respuesta de la API).
type PosicionCola struct {
	Posicion        int64      `json:"posicion"`
	EnCola          int64      `json:"en_cola"`
	TurnoHabilitado bool       `json:"turno_habilitado"`
	EsperaMin       int        `json:"espera_min"`
	Expira          *time.Time `json:"expira,omitempty"`     // fin del turno, si lo tiene
	Confirmado      bool       `json:"confirmado"`           // ya bloqueó butacas: el turno dura completo (si no, es el minuto de gracia)
	Capacidad       int64      `json:"capacidad"`            // clientes atendidos a la vez
	ButacasLibres   int        `json:"butacas_libres"`       // a la venta y sin tomar en este momento
	Pronostico      string     `json:"pronostico,omitempty"` // quien espera: alcanza | incierto | no-alcanza
	Agotado         bool       `json:"agotado,omitempty"`    // no queda ninguna butaca: la cola se cerró
	Cerrada         bool       `json:"cerrada,omitempty"`    // la función ya comenzó
}

// OcupacionFuncion resume la venta de una función para el panel de métricas.
type OcupacionFuncion struct {
	Codigo   string `json:"codigo"`
	Vendidos int    `json:"vendidos"`
	Aforo    int    `json:"aforo"`
	Ingresos int    `json:"ingresos"`
}

// Metricas del panel de administración (RF-56).
type Metricas struct {
	VendidosHoy       int                `json:"vendidos_hoy"`
	IngresosHoy       int                `json:"ingresos_hoy"`
	AsientosRetenidos int                `json:"asientos_retenidos"`
	Colas             map[string]int64   `json:"colas"`
	CreditosTrivia    int                `json:"creditos_trivia"`
	CreditosCanjeados int                `json:"creditos_canjeados"`
	Cuentas           int                `json:"cuentas"`
	Funciones         []OcupacionFuncion `json:"funciones"`
}

// ConsumoPelicula resume lo que un cliente compró de una película. Género, director y reparto sólo
// vienen llenos en las películas creadas desde el panel; los de la cartelera de demostración los completa el frontend.
type ConsumoPelicula struct {
	Pelicula  string    `json:"pelicula"` // slug
	Titulo    string    `json:"titulo"`
	Genero    string    `json:"genero"`
	Director  string    `json:"director"`
	Reparto   []string  `json:"reparto"`
	Boletos   int       `json:"boletos"`
	Funciones int       `json:"funciones"` // visitas: funciones distintas a las que asistió
	Gastado   int       `json:"gastado"`
	Ultima    time.Time `json:"ultima"`
}

// HistorialCliente agrupa las compras de un cliente por película (panel de estadísticas de clientes).
type HistorialCliente struct {
	ID        int64             `json:"id"`
	Nombre    string            `json:"nombre"`
	Correo    string            `json:"correo"`
	Activo    bool              `json:"activo"`
	Creado    time.Time         `json:"creado"`
	Saldo     int               `json:"saldo"`
	Peliculas []ConsumoPelicula `json:"peliculas"`
}

// TipoEventoAsiento describe un cambio de estado de butacas que se difunde en tiempo real.
type TipoEventoAsiento string

const (
	AsientoBloqueado TipoEventoAsiento = "bloqueado" // alguien la tomó (clic en el mapa o reserva)
	AsientoLiberado  TipoEventoAsiento = "liberado"  // se soltó, se canceló o venció el TTL
	AsientoVendido   TipoEventoAsiento = "vendido"   // pago confirmado
	AsientoRetirado  TipoEventoAsiento = "retirado"  // el personal la marcó fuera de servicio
)

// EventoAsientos es el mensaje que reciben los clientes conectados por SSE.
type EventoAsientos struct {
	Tipo     TipoEventoAsiento `json:"tipo"`
	Funcion  string            `json:"funcion"`
	Asientos []string          `json:"asientos"`
}
