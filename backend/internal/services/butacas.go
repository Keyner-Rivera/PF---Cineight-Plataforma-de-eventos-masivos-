package services

import (
	"context"
	"errors"
	"strings"

	"github.com/cineight/backend/internal/domain"
)

// Butacas permite al personal retirar butacas de la venta (dañadas o fuera de servicio) y devolverlas.
// Una butaca retirada no se puede bloquear, reservar ni pagar en ninguna función de su sala.
type Butacas struct {
	repo      Mantenimiento
	asientos  *Asientos
	auditoria Auditoria
	alCambiar func(context.Context) // invalida la caché de la cartelera (cambian las butacas disponibles)
}

// NuevoButacas crea el servicio.
func NuevoButacas(r Mantenimiento, a *Asientos, au Auditoria, alCambiar func(context.Context)) *Butacas {
	return &Butacas{repo: r, asientos: a, auditoria: au, alCambiar: alCambiar}
}

// FueraServicio lista las butacas retiradas de una sala.
func (s *Butacas) FueraServicio(ctx context.Context, sala string) ([]domain.ButacaFueraServicio, error) {
	lista, err := s.repo.ButacasFueraServicio(ctx, sala)
	if errors.Is(err, domain.ErrNoExiste) {
		return nil, domain.NoEncontrado("La sala no existe.")
	}
	return lista, err
}

// Retirar marca una butaca como dañada o fuera de servicio. Las butacas ya vendidas conservan su boleto.
func (s *Butacas) Retirar(ctx context.Context, sala, asiento, motivo, por string) error {
	if !domain.FormatoAsiento.MatchString(asiento) || !domain.MotivosFueraServicio[motivo] {
		return domain.Invalido("Butaca o motivo inválidos.")
	}
	if r := []rune(strings.TrimSpace(por)); len(r) > 120 {
		por = string(r[:120])
	} else {
		por = string(r)
	}
	if err := s.repo.MarcarFueraServicio(ctx, sala, asiento, motivo, por); err != nil {
		if errors.Is(err, domain.ErrNoExiste) {
			return domain.NoEncontrado("La butaca " + asiento + " no existe en esa sala.")
		}
		return err
	}
	s.auditoria.Auditar(ctx, nil, "BUTACA_FUERA_SERVICIO", "butacas_fuera_servicio", map[string]string{"sala": sala, "asiento": asiento, "motivo": motivo, "por": por})
	s.avisar(ctx, sala, domain.AsientoRetirado, asiento)
	return nil
}

// Habilitar devuelve una butaca a la venta.
func (s *Butacas) Habilitar(ctx context.Context, sala, asiento, por string) error {
	if !domain.FormatoAsiento.MatchString(asiento) {
		return domain.Invalido("Butaca inválida.")
	}
	if err := s.repo.HabilitarButaca(ctx, sala, asiento); err != nil {
		if errors.Is(err, domain.ErrNoExiste) {
			return domain.NoEncontrado("La butaca " + asiento + " no estaba fuera de servicio.")
		}
		return err
	}
	s.auditoria.Auditar(ctx, nil, "BUTACA_HABILITADA", "butacas_fuera_servicio", map[string]string{"sala": sala, "asiento": asiento, "por": strings.TrimSpace(por)})
	s.avisar(ctx, sala, domain.AsientoLiberado, asiento)
	return nil
}

// avisar difunde el cambio a los mapas abiertos de las funciones por venir de la sala y renueva la cartelera.
func (s *Butacas) avisar(ctx context.Context, sala string, tipo domain.TipoEventoAsiento, asiento string) {
	if s.alCambiar != nil {
		s.alCambiar(ctx)
	}
	funciones, err := s.repo.FuncionesVigentesDeSala(ctx, sala)
	if err != nil {
		return
	}
	for id, codigo := range funciones {
		s.asientos.publicar(ctx, id, codigo, tipo, []string{asiento})
	}
}
