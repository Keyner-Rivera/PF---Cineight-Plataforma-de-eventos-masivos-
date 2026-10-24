package services

import (
	"context"
	"errors"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// CuentasClientes vincula clientes con su cuenta de créditos, acredita la trivia y valida boletos.
type CuentasClientes struct {
	cuentas   Cuentas
	ventas    Ventas
	auditoria Auditoria
	guardia   GuardiaAntiBots
}

// NuevoCuentas crea el servicio.
func NuevoCuentas(c Cuentas, v Ventas, a Auditoria, g GuardiaAntiBots) *CuentasClientes {
	return &CuentasClientes{cuentas: c, ventas: v, auditoria: a, guardia: g}
}

// Sesion vincula al cliente que inició sesión en el frontend con su cuenta en PostgreSQL
// (la crea con el bono de bienvenida). Pendiente: autenticación real con JWT + bcrypt.
// Crear una cuenta regala créditos, por eso exige el pase de reCAPTCHA: sin él un bot podría llamar
// directamente a esta ruta y fabricar cuentas. Vincular una cuenta existente no lo necesita (p. ej. al recargar la página).
func (s *CuentasClientes) Sesion(ctx context.Context, correo, nombre, pase string) (int64, int, error) {
	correo = domain.NormalizarCorreo(correo)
	if err := domain.ValidarCorreo(correo); err != nil {
		return 0, 0, err
	}
	nombre = domain.NormalizarNombre(nombre)
	if err := domain.ValidarNombre(nombre); err != nil {
		return 0, 0, err
	}
	existe, err := s.cuentas.ExisteCliente(ctx, correo)
	if err != nil {
		return 0, 0, err
	}
	if !existe {
		if err := s.guardia.ExigirPase(ctx, pase, "alta de cuenta"); err != nil {
			return 0, 0, err
		}
	}
	return s.cuentas.VincularCliente(ctx, correo, nombre)
}

// Cuenta devuelve saldo, movimientos y boletos.
func (s *CuentasClientes) Cuenta(ctx context.Context, usuarioID int64) (domain.Cuenta, error) {
	c, err := s.cuentas.Cuenta(ctx, usuarioID)
	if errors.Is(err, domain.ErrNoExiste) {
		return c, domain.NoEncontrado("La cuenta no existe.")
	}
	return c, err
}

// Trivia acredita el premio de una partida. El monto lo calcula el servidor a partir de los aciertos
// (el cliente no decide cuánto cobra) y la referencia única evita acreditarla dos veces (RF-64).
func (s *CuentasClientes) Trivia(ctx context.Context, usuarioID int64, partida string, aciertos int) (acreditado bool, monto, saldo int, err error) {
	if usuarioID == 0 || partida == "" || aciertos < 0 || aciertos > domain.PreguntasTrivia {
		return false, 0, 0, domain.Invalido("Partida inválida.")
	}
	monto = aciertos * domain.CreditosPorAcierto
	if aciertos == domain.PreguntasTrivia {
		monto = domain.BonoTriviaPerfecta
	}
	acreditado, saldo, err = s.cuentas.AcreditarTrivia(ctx, usuarioID, partida, monto)
	return
}

// ValidarBoleto marca el boleto como utilizado si el código dinámico del QR es vigente, y registra el intento en la
// bitácora (RF-09, RF-27). El código se comprueba aquí y no sólo en el navegador del agente: sin él, quien conozca el
// token podría marcarlo como usado llamando directamente a la API, y una captura de pantalla vieja serviría para entrar.
// Un código vencido o falso no consume el boleto.
func (s *CuentasClientes) ValidarBoleto(ctx context.Context, tok, codigo string, ventana int64) (domain.Validacion, error) {
	if !domain.CodigoQRVigente(tok, codigo, ventana, time.Now()) {
		s.auditoria.Auditar(ctx, nil, "VALIDACION_ACCESO", "boletos", map[string]any{"token": tok, "ok": false, "motivo": "codigo QR vencido"})
		return domain.Validacion{OK: false, Motivo: "Código QR vencido o inválido (posible captura de pantalla). Pide al cliente que abra de nuevo su entrada."}, nil
	}
	v, err := s.ventas.ValidarBoleto(ctx, tok)
	if err == nil {
		s.auditoria.Auditar(ctx, nil, "VALIDACION_ACCESO", "boletos", map[string]any{"token": tok, "ok": v.OK})
	}
	return v, err
}
