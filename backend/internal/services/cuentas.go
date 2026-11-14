package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// CuentasClientes consulta la cuenta de créditos del cliente, acredita la trivia, entrega los códigos de sus QR y valida boletos.
type CuentasClientes struct {
	cuentas   Cuentas
	ventas    Ventas
	auditoria Auditoria
	secretoQR []byte // firma los códigos dinámicos de los QR (HMAC-SHA256)
}

// NuevoCuentas crea el servicio.
func NuevoCuentas(c Cuentas, v Ventas, a Auditoria, secretoQR []byte) *CuentasClientes {
	return &CuentasClientes{cuentas: c, ventas: v, auditoria: a, secretoQR: secretoQR}
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
	if usuarioID == 0 || partida == "" || len(partida) > 64 || aciertos < 0 || aciertos > domain.PreguntasTrivia {
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
// bitácora (RF-09, RF-27). El código se comprueba en el servidor con su secreto: sin él, quien conozca el token no puede
// fabricar un QR válido, y una captura de pantalla vieja no sirve para entrar. Un código vencido o falso no consume el boleto.
func (s *CuentasClientes) ValidarBoleto(ctx context.Context, tok, codigo string, ventana int64, porUsuario int64) (domain.Validacion, error) {
	if !domain.CodigoQRVigente(s.secretoQR, tok, codigo, ventana, time.Now()) {
		s.auditoria.Auditar(ctx, &porUsuario, "VALIDACION_ACCESO", "boletos", map[string]any{"token": enmascarar(tok), "ok": false, "motivo": "codigo QR vencido"})
		return domain.Validacion{OK: false, Motivo: "Código QR vencido o inválido (posible captura de pantalla). Pide al cliente que abra de nuevo su entrada."}, nil
	}
	v, err := s.ventas.ValidarBoleto(ctx, tok)
	if err == nil {
		s.auditoria.Auditar(ctx, &porUsuario, "VALIDACION_ACCESO", "boletos", map[string]any{"token": enmascarar(tok), "ok": v.OK, "motivo": v.Motivo})
	}
	return v, err
}

// CodigosQR entrega al dueño de un boleto válido los códigos de los próximos minutos para mostrar su QR.
// Para cualquier otro usuario el boleto "no existe" (no se revela si el token es real).
func (s *CuentasClientes) CodigosQR(ctx context.Context, tok string, usuarioID int64) (desde int64, codigos []string, err error) {
	estado, err := s.ventas.BoletoDe(ctx, tok, usuarioID)
	if errors.Is(err, domain.ErrNoExiste) {
		return 0, nil, domain.NoEncontrado("El boleto no existe.")
	}
	if err != nil {
		return 0, nil, err
	}
	if estado != "valido" {
		return 0, nil, domain.Conflicto("Esta entrada ya no permite el acceso.")
	}
	desde, codigos = domain.CodigosQR(s.secretoQR, tok, time.Now())
	return desde, codigos, nil
}

// enmascarar deja visibles sólo los extremos del token en la bitácora.
func enmascarar(tok string) string {
	if len(tok) < 10 {
		return "…"
	}
	return tok[:6] + "…" + tok[len(tok)-3:]
}

// ───────── Comprobantes de compra ─────────

// formatoFolio son los folios que emite el servidor (id de reserva): evita consultar la base con cualquier texto.
var formatoFolio = regexp.MustCompile(`^R-[0-9A-F]{12}$`)

// firmarComprobante es la firma del comprobante: HMAC-SHA256, con el secreto del servidor, de los datos que lo
// identifican (folio, fecha, función, butacas y total). Cambiar cualquiera de ellos invalida la firma.
func (s *CuentasClientes) firmarComprobante(c domain.Comprobante) string {
	m := hmac.New(sha256.New, s.secretoQR)
	fmt.Fprintf(m, "comprobante|%s|%d|%s|%s|%d", c.Folio, c.Fecha.Unix(), c.Funcion, strings.Join(c.Asientos, ","), c.Total)
	return base32.StdEncoding.EncodeToString(m.Sum(nil))[:16]
}

// Comprobante devuelve el comprobante firmado de una compra del usuario (para otro usuario "no existe").
func (s *CuentasClientes) Comprobante(ctx context.Context, folio string, usuarioID int64) (domain.Comprobante, error) {
	if !formatoFolio.MatchString(folio) || usuarioID <= 0 {
		return domain.Comprobante{}, domain.NoEncontrado("El comprobante no existe.")
	}
	c, err := s.ventas.Comprobante(ctx, folio, usuarioID)
	if errors.Is(err, domain.ErrNoExiste) {
		return c, domain.NoEncontrado("El comprobante no existe.")
	}
	if err != nil {
		return c, err
	}
	c.Firma = s.firmarComprobante(c)
	return c, nil
}

// VerificarComprobante comprueba la firma de un comprobante (lo que hace cualquiera al escanear su QR). Responde sin
// datos personales completos ni los códigos de las entradas: sólo si es auténtico y qué ampara.
func (s *CuentasClientes) VerificarComprobante(ctx context.Context, folio, firma string) (domain.VerificacionComprobante, error) {
	if !formatoFolio.MatchString(folio) {
		return domain.VerificacionComprobante{Motivo: "Comprobante inexistente."}, nil
	}
	c, err := s.ventas.Comprobante(ctx, folio, 0)
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.VerificacionComprobante{Motivo: "Comprobante inexistente."}, nil
	}
	if err != nil {
		return domain.VerificacionComprobante{}, err
	}
	if !hmac.Equal([]byte(strings.ToUpper(strings.TrimSpace(firma))), []byte(s.firmarComprobante(c))) {
		return domain.VerificacionComprobante{Motivo: "Firma inválida: el comprobante fue alterado o no lo emitió Cineight."}, nil
	}
	v := domain.VerificacionComprobante{Valido: true, Motivo: "Comprobante auténtico emitido por Cineight."}
	for _, b := range c.Boletos {
		if b.Estado == "valido" {
			v.Vigentes++
		} else if b.Estado == "utilizado" {
			v.Utilizados++
		}
	}
	c.Correo, c.Boletos = "", nil
	c.Cliente = enmascararNombre(c.Cliente)
	v.Comprobante = &c
	return v, nil
}

// enmascararNombre deja el primer nombre y la inicial del apellido ("Ana López" → "Ana L.").
func enmascararNombre(n string) string {
	partes := strings.Fields(n)
	if len(partes) < 2 {
		return n
	}
	return partes[0] + " " + string([]rune(partes[1])[0]) + "."
}
