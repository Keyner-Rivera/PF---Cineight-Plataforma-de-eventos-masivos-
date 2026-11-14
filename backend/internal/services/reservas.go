package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/pkg/token"
)

// Reservas implementa el flujo de compra: retención de 5 min en PostgreSQL (bloqueo transaccional, fuente de verdad)
// y en Redis (tiempo real), y pago atómico en PostgreSQL que vuelve a verificar todo y recalcula el precio.
type Reservas struct {
	asientos    *Asientos
	catalogo    Catalogo
	candados    Candados
	retenciones Retenciones
	ventas      Ventas
	retencion   time.Duration
}

// NuevoReservas crea el servicio.
func NuevoReservas(a *Asientos, c Catalogo, k Candados, rt Retenciones, v Ventas, retencion time.Duration) *Reservas {
	return &Reservas{asientos: a, catalogo: c, candados: k, retenciones: rt, ventas: v, retencion: retencion}
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
func (s *Reservas) Crear(ctx context.Context, sol SolicitudReserva) (domain.ReservaCreada, error) {
	var nada domain.ReservaCreada
	if err := validarSolicitud(sol); err != nil {
		return nada, err
	}
	ref := sol.Funcion
	if ref == "" {
		ref = fmt.Sprint(sol.FuncionID)
	}
	fid, err := s.asientos.Resolver(ctx, ref)
	if err != nil {
		return nada, err
	}

	evento, vendidos, propios, err := s.catalogo.ContextoReserva(ctx, fid, sol.Asientos, sol.UsuarioID)
	if err != nil {
		return nada, errorDeFuncion(err)
	}
	if err := s.asientos.turnos.ExigirTurno(ctx, evento, sol.UsuarioID); err != nil {
		return nada, err
	}
	if vendidos > 0 {
		return nada, domain.Conflicto("Alguno de los asientos ya no está disponible.")
	}
	// Límite por cliente en preestrenos de alta demanda (RF-36). La transacción de la base lo vuelve a comprobar.
	if evento != nil && propios+len(sol.Asientos) > domain.MaxBoletosPorEvento {
		return nada, domain.Conflicto(fmt.Sprintf("El preestreno permite %d boletos por cliente (ya tienes %d).", domain.MaxBoletosPorEvento, propios))
	}

	r := domain.Reserva{ID: "R-" + token.Nuevo(6), FuncionID: fid, UsuarioID: sol.UsuarioID, Asientos: sol.Asientos}
	ocupada, err := s.candados.Retener(ctx, r, s.retencion)
	if err != nil {
		return nada, err
	}
	if ocupada != "" {
		return nada, domain.Conflicto("La butaca " + ocupada + " ya no está disponible.")
	}
	// Bloqueo transaccional en PostgreSQL: todas las butacas o ninguna, verificadas con la función bloqueada.
	precio, err := s.retenciones.RetenerReserva(ctx, r, s.retencion, domain.MaxBoletosPorEvento)
	if err != nil {
		_, _ = s.candados.BorrarReserva(ctx, r)
		return nada, errorDeRetencion(err, "")
	}
	if err := s.candados.GuardarReserva(ctx, r, s.retencion); err != nil {
		_, _ = s.candados.BorrarReserva(ctx, r)
		_ = s.retenciones.LiberarReserva(ctx, r.ID, r.UsuarioID)
		return nada, err
	}
	s.asientos.turnos.ConfirmarTurno(ctx, evento, sol.UsuarioID)
	s.asientos.publicar(ctx, fid, ref, domain.AsientoBloqueado, r.Asientos)
	// El precio lo fija el servidor con la tarifa vigente de la función; al pagar se vuelve a calcular.
	return domain.ReservaCreada{ID: r.ID, Expira: time.Now().Add(s.retencion), PrecioUnitario: precio, Total: precio * len(r.Asientos)}, nil
}

// validarSolicitud comprueba la forma de la solicitud antes de tocar la base de datos o Redis.
func validarSolicitud(sol SolicitudReserva) error {
	if len(sol.Asientos) == 0 || len(sol.Asientos) > domain.MaxAsientosPorCompra || sol.UsuarioID == 0 {
		return domain.Invalido(fmt.Sprintf("Solicitud inválida: entre 1 y %d asientos y usuario_id requeridos.", domain.MaxAsientosPorCompra))
	}
	vistas := map[string]bool{}
	for _, as := range sol.Asientos {
		if !domain.FormatoAsiento.MatchString(as) {
			return domain.Invalido("Butaca inválida: " + as)
		}
		if vistas[as] {
			return domain.Invalido("Butaca repetida: " + as)
		}
		vistas[as] = true
	}
	return nil
}

// Cancelar libera las butacas de una reserva vigente del usuario.
func (s *Reservas) Cancelar(ctx context.Context, id string, usuarioID int64) error {
	r, err := s.candados.Reserva(ctx, id)
	if errors.Is(err, domain.ErrNoExiste) || (err == nil && r.UsuarioID != usuarioID) {
		return domain.NoEncontrado("La reserva no existe o ya expiró.")
	}
	if err != nil {
		return err
	}
	if err := s.retenciones.LiberarReserva(ctx, r.ID, usuarioID); err != nil {
		return err
	}
	liberadas, err := s.candados.BorrarReserva(ctx, r)
	if err != nil {
		return err
	}
	s.asientos.publicar(ctx, r.FuncionID, "", domain.AsientoLiberado, liberadas)
	return nil
}

// Pagar confirma una reserva del usuario. Es idempotente (RF-23): repetir el pago devuelve los mismos boletos sin
// volver a cobrar. La reserva de otro usuario "no existe": no se puede pagar con sus créditos ni ver sus boletos.
func (s *Reservas) Pagar(ctx context.Context, id string, usuarioID int64) (domain.ResultadoPago, int, error) {
	if previos := s.ventas.BoletosDeReserva(ctx, id, usuarioID); len(previos) > 0 {
		return domain.ResultadoPago{ReservaID: id, Boletos: previos, Repetido: true}, 200, nil
	}
	r, err := s.candados.Reserva(ctx, id)
	if err == nil && r.UsuarioID != usuarioID {
		return domain.ResultadoPago{}, 0, domain.NoEncontrado("La reserva no existe.")
	}
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.ResultadoPago{}, 0, domain.Expirado("La reserva expiró y los asientos fueron liberados.")
	}
	if err != nil {
		return domain.ResultadoPago{}, 0, err
	}

	saldo, total, err := s.ventas.RegistrarVenta(ctx, r, domain.MaxBoletosPorEvento)
	switch {
	case errors.Is(err, domain.ErrSaldoInsuficiente):
		return domain.ResultadoPago{}, 0, domain.SinSaldo("Saldo de créditos insuficiente.")
	case errors.Is(err, domain.ErrReservaVencida), errors.Is(err, domain.ErrFuncionIniciada):
		// Pagos repetidos simultáneos: esperaron el bloqueo de la función y, al entrar, el primero ya había cobrado y
		// borrado la retención. Devuelven los mismos boletos sin volver a cobrar (RF-23).
		if previos := s.ventas.BoletosDeReserva(ctx, id, usuarioID); len(previos) > 0 {
			return domain.ResultadoPago{ReservaID: id, Boletos: previos, Repetido: true}, 200, nil
		}
		// La base dice que la reserva ya no vale (venció o la función empezó): se liberan sus candados en Redis.
		liberadas, _ := s.candados.BorrarReserva(ctx, r)
		s.asientos.publicar(ctx, r.FuncionID, "", domain.AsientoLiberado, liberadas)
		return domain.ResultadoPago{}, 0, errorDeRetencion(err, "")
	case errors.Is(err, domain.ErrLimiteEvento):
		return domain.ResultadoPago{}, 0, errorDeRetencion(err, "")
	case errors.Is(err, domain.ErrAsientoVendido):
		// Otro proceso vendió la butaca o el pago ya se registró; la transacción se revirtió completa.
		if previos := s.ventas.BoletosDeReserva(ctx, id, usuarioID); len(previos) > 0 {
			return domain.ResultadoPago{ReservaID: id, Boletos: previos, Repetido: true}, 200, nil
		}
		return domain.ResultadoPago{}, 0, domain.Conflicto("El asiento fue vendido a otro comprador; no se debitaron créditos.")
	case err != nil:
		return domain.ResultadoPago{}, 0, err
	}

	_, _ = s.candados.BorrarReserva(ctx, r)
	s.asientos.publicar(ctx, r.FuncionID, "", domain.AsientoVendido, r.Asientos)
	return domain.ResultadoPago{ReservaID: id, Boletos: s.ventas.BoletosDeReserva(ctx, id, usuarioID), Saldo: &saldo, Total: total}, 201, nil
}

// MantenerRetenciones libera cada 30 s las retenciones de la base cuyo tiempo terminó (selecciones y reservas no
// pagadas) y avisa a los mapas abiertos. Redis vence sus candados solo; esto mantiene la base igual de limpia aunque
// nadie vuelva a tocar esas butacas. Corre hasta que ctx se cancela.
func (s *Reservas) MantenerRetenciones(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		liberadas, err := s.retenciones.LimpiarRetencionesVencidas(ctx)
		if err != nil {
			log.Printf("retenciones: no se pudieron limpiar las vencidas: %v", err)
			continue
		}
		for fid, asientos := range liberadas {
			s.asientos.publicar(ctx, fid, "", domain.AsientoLiberado, asientos)
		}
	}
}
