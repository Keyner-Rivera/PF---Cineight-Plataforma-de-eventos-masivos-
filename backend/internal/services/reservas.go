package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/pkg/token"
)

// Reservas implementa el flujo de compra: retención de 5 min en Redis y pago atómico en PostgreSQL.
type Reservas struct {
	asientos  *Asientos
	catalogo  Catalogo
	candados  Candados
	ventas    Ventas
	retencion time.Duration
}

// NuevoReservas crea el servicio.
func NuevoReservas(a *Asientos, c Catalogo, k Candados, v Ventas, retencion time.Duration) *Reservas {
	return &Reservas{asientos: a, catalogo: c, candados: k, ventas: v, retencion: retencion}
}

// SolicitudReserva es el cuerpo de POST /api/reservas.
type SolicitudReserva struct {
	Funcion   string   `json:"funcion"`
	FuncionID int      `json:"funcion_id"`
	UsuarioID int64    `json:"usuario_id"`
	Asientos  []string `json:"asientos"`
}

// Crear retiene las butacas a nombre de una reserva nueva. Las butacas deben estar libres
// o bloqueadas por el mismo usuario desde el mapa; la operación es todo o nada (RF-54).
func (s *Reservas) Crear(ctx context.Context, sol SolicitudReserva) (id string, expira time.Time, err error) {
	if err := validarSolicitud(sol); err != nil {
		return "", expira, err
	}
	ref := sol.Funcion
	if ref == "" {
		ref = fmt.Sprint(sol.FuncionID)
	}
	fid, err := s.asientos.Resolver(ctx, ref)
	if err != nil {
		return "", expira, err
	}

	evento, vendidos, propios, err := s.catalogo.ContextoReserva(ctx, fid, sol.Asientos, sol.UsuarioID)
	if err != nil {
		return "", expira, errorDeFuncion(err)
	}
	if err := s.asientos.turnos.ExigirTurno(ctx, evento, sol.UsuarioID); err != nil {
		return "", expira, err
	}
	if vendidos > 0 {
		return "", expira, domain.Conflicto("Alguno de los asientos ya no está disponible.")
	}
	// Límite por cliente en preestrenos de alta demanda (RF-36).
	if evento != nil && propios+len(sol.Asientos) > domain.MaxBoletosPorEvento {
		return "", expira, domain.Conflicto(fmt.Sprintf("El preestreno permite %d boletos por cliente (ya tienes %d).", domain.MaxBoletosPorEvento, propios))
	}

	r := domain.Reserva{ID: "R-" + token.Nuevo(6), FuncionID: fid, UsuarioID: sol.UsuarioID, Asientos: sol.Asientos}
	ocupada, err := s.candados.Retener(ctx, r, s.retencion)
	if err != nil {
		return "", expira, err
	}
	if ocupada != "" {
		return "", expira, domain.Conflicto("La butaca " + ocupada + " ya no está disponible.")
	}
	if err := s.candados.GuardarReserva(ctx, r, s.retencion); err != nil {
		_, _ = s.candados.BorrarReserva(ctx, r)
		return "", expira, err
	}
	s.asientos.turnos.ConfirmarTurno(ctx, evento, sol.UsuarioID)
	s.asientos.publicar(ctx, fid, ref, domain.AsientoBloqueado, r.Asientos)
	return r.ID, time.Now().Add(s.retencion), nil
}

// validarSolicitud comprueba la forma de la solicitud antes de tocar la base de datos o Redis.
func validarSolicitud(sol SolicitudReserva) error {
	if len(sol.Asientos) == 0 || len(sol.Asientos) > domain.MaxAsientosPorCompra || sol.UsuarioID == 0 {
		return domain.Invalido(fmt.Sprintf("Solicitud inválida: entre 1 y %d asientos y usuario_id requeridos.", domain.MaxAsientosPorCompra))
	}
	for _, as := range sol.Asientos {
		if !domain.FormatoAsiento.MatchString(as) {
			return domain.Invalido("Butaca inválida: " + as)
		}
	}
	return nil
}

// Cancelar libera las butacas de una reserva vigente.
func (s *Reservas) Cancelar(ctx context.Context, id string) error {
	r, err := s.candados.Reserva(ctx, id)
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.NoEncontrado("La reserva no existe o ya expiró.")
	}
	if err != nil {
		return err
	}
	liberadas, err := s.candados.BorrarReserva(ctx, r)
	if err != nil {
		return err
	}
	s.asientos.publicar(ctx, r.FuncionID, "", domain.AsientoLiberado, liberadas)
	return nil
}

// Pagar confirma la reserva. Es idempotente (RF-23): repetir el pago devuelve los mismos boletos sin volver a cobrar.
func (s *Reservas) Pagar(ctx context.Context, id string) (domain.ResultadoPago, int, error) {
	if previos := s.ventas.BoletosDeReserva(ctx, id); len(previos) > 0 {
		return domain.ResultadoPago{ReservaID: id, Boletos: previos, Repetido: true}, 200, nil
	}
	r, err := s.candados.Reserva(ctx, id)
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.ResultadoPago{}, 0, domain.Expirado("La reserva expiró y los asientos fueron liberados.")
	}
	if err != nil {
		return domain.ResultadoPago{}, 0, err
	}

	saldo, err := s.ventas.RegistrarVenta(ctx, r)
	switch {
	case errors.Is(err, domain.ErrSaldoInsuficiente):
		return domain.ResultadoPago{}, 0, domain.SinSaldo("Saldo de créditos insuficiente.")
	case errors.Is(err, domain.ErrAsientoVendido):
		// Otro proceso vendió la butaca o el pago ya se registró; la transacción se revirtió completa.
		if previos := s.ventas.BoletosDeReserva(ctx, id); len(previos) > 0 {
			return domain.ResultadoPago{ReservaID: id, Boletos: previos, Repetido: true}, 200, nil
		}
		return domain.ResultadoPago{}, 0, domain.Conflicto("El asiento fue vendido a otro comprador; no se debitaron créditos.")
	case err != nil:
		return domain.ResultadoPago{}, 0, err
	}

	_, _ = s.candados.BorrarReserva(ctx, r)
	s.asientos.publicar(ctx, r.FuncionID, "", domain.AsientoVendido, r.Asientos)
	return domain.ResultadoPago{ReservaID: id, Boletos: s.ventas.BoletosDeReserva(ctx, id), Saldo: &saldo}, 201, nil
}
