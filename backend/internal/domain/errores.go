package domain

import (
	"errors"
	"net/http"
	"time"
)

// Error de negocio con el código HTTP con que debe responderse y un mensaje apto para el usuario.
// Causa (opcional) identifica el tipo de error para errors.Is, sin depender del texto del mensaje.
type Error struct {
	Estado  int
	Mensaje string
	Causa   error
}

func (e *Error) Error() string { return e.Mensaje }
func (e *Error) Unwrap() error { return e.Causa }

// Constructores de errores frecuentes.
func Invalido(msg string) error     { return &Error{Estado: http.StatusBadRequest, Mensaje: msg} }
func NoEncontrado(msg string) error { return &Error{Estado: http.StatusNotFound, Mensaje: msg} }
func Conflicto(msg string) error    { return &Error{Estado: http.StatusConflict, Mensaje: msg} }
func Expirado(msg string) error     { return &Error{Estado: http.StatusGone, Mensaje: msg} }
func SinSaldo(msg string) error     { return &Error{Estado: http.StatusPaymentRequired, Mensaje: msg} }
func Prohibido(msg string) error    { return &Error{Estado: http.StatusForbidden, Mensaje: msg} }
func Demasiadas(msg string) error   { return &Error{Estado: http.StatusTooManyRequests, Mensaje: msg} }

// invalidoPor es un 400 que además se reconoce con errors.Is(err, causa).
func invalidoPor(causa error, msg string) error {
	return &Error{Estado: http.StatusBadRequest, Mensaje: msg, Causa: causa}
}

// ErrCorreoInvalido y ErrNombreInvalido identifican los rechazos de ValidarCorreo y ValidarNombre.
var (
	ErrCorreoInvalido = errors.New("correo inválido")
	ErrNombreInvalido = errors.New("nombre inválido")
)

// ErrSaldoInsuficiente se usa dentro de la transacción de pago para revertirla.
var ErrSaldoInsuficiente = errors.New("saldo insuficiente")

// ErrAsientoVendido: la venta violó la unicidad de butaca (otro comprador ganó o el pago ya existía).
var ErrAsientoVendido = errors.New("asiento ya vendido")

// ErrTraslape: la función se cruza con otra de la misma sala (incluida la limpieza).
type ErrTraslape struct {
	Sala, Titulo string
	Inicio, Fin  time.Time
}

func (e *ErrTraslape) Error() string {
	return "traslape en " + e.Sala + " con " + e.Titulo
}

// ErrNoExiste indica que un registro buscado no está en el almacenamiento.
var ErrNoExiste = errors.New("no existe")

// ErrFuncionIniciada: la función ya empezó (o se canceló) y no admite más ventas.
var ErrFuncionIniciada = errors.New("la función ya comenzó")

// ErrAsientoInexistente: la butaca tiene formato válido pero no existe en la sala de la función (p. ej. J-12 en una
// sala de 6 filas). Sin esta revisión se podrían vender butacas fantasma y superar el aforo.
var ErrAsientoInexistente = errors.New("la butaca no existe en la sala")

// ErrNoDisponible envuelve las fallas de conexión con Redis: la API responde 503 en lugar de un error interno.
var ErrNoDisponible = errors.New("servicio de reservas no disponible")

// MensajeNoDisponible es el texto que recibe el cliente cuando Redis no responde.
const MensajeNoDisponible = "Servicio de reservas temporalmente no disponible"
