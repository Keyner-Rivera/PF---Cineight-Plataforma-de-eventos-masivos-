package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// Asientos gestiona el mapa de butacas en tiempo real: candados individuales (SET NX EX)
// y la difusión de cada cambio a los clientes conectados por SSE.
type Asientos struct {
	catalogo  Catalogo
	candados  Candados
	turnos    Turnos
	retencion time.Duration
}

// NuevoAsientos crea el servicio. `turnos` exige turno de la cola virtual en las funciones de preestreno.
func NuevoAsientos(c Catalogo, k Candados, t Turnos, retencion time.Duration) *Asientos {
	return &Asientos{catalogo: c, candados: k, turnos: t, retencion: retencion}
}

// Resolver convierte el código público o el id de una función en su id numérico.
func (s *Asientos) Resolver(ctx context.Context, ref string) (int, error) {
	id, err := s.catalogo.FuncionID(ctx, ref)
	if err != nil {
		return 0, domain.NoEncontrado("La función no existe.")
	}
	return id, nil
}

// errorDeFuncion traduce los errores de ContextoReserva a mensajes para el cliente.
func errorDeFuncion(err error) error {
	switch {
	case errors.Is(err, domain.ErrNoExiste):
		return domain.NoEncontrado("La función no existe.")
	case errors.Is(err, domain.ErrFuncionIniciada):
		return domain.Expirado("Esta función ya comenzó. Elige otro horario.")
	case errors.Is(err, domain.ErrAsientoInexistente):
		return domain.Invalido("Esa butaca no existe en la sala de esta función.")
	}
	return err
}

// Ocupacion combina las butacas vendidas y fuera de servicio (PostgreSQL) y las retenidas o bloqueadas (claves vivas en Redis).
func (s *Asientos) Ocupacion(ctx context.Context, funcionID int) (domain.Ocupacion, error) {
	vendidos, err := s.catalogo.AsientosVendidos(ctx, funcionID)
	if err != nil {
		return domain.Ocupacion{}, err
	}
	fuera, err := s.catalogo.FueraServicio(ctx, funcionID)
	if err != nil {
		return domain.Ocupacion{}, err
	}
	if fuera == nil {
		fuera = []string{}
	}
	return domain.Ocupacion{Vendidos: vendidos, Retenidos: s.candados.Retenidos(ctx, funcionID), FueraServicio: fuera}, nil
}

// BloqueosDe lista las butacas que el usuario tiene bloqueadas en la función.
func (s *Asientos) BloqueosDe(ctx context.Context, funcionID int, usuarioID int64) []string {
	return s.candados.BloqueosDe(ctx, funcionID, usuarioID)
}

// OcupacionVigente devuelve la ocupación de todas las funciones vigentes, indexada por código público.
func (s *Asientos) OcupacionVigente(ctx context.Context) (map[string]domain.Ocupacion, error) {
	codigos, ocupacion, err := s.catalogo.VendidosVigentes(ctx)
	if err != nil {
		return nil, err
	}
	retenidos := s.candados.RetenidosPorFuncion(ctx)
	out := make(map[string]domain.Ocupacion, len(codigos))
	for id, codigo := range codigos {
		o := ocupacion[id]
		o.Retenidos = retenidos[id]
		if o.Retenidos == nil {
			o.Retenidos = []string{}
		}
		out[codigo] = o
	}
	return out, nil
}

// Bloquear toma una butaca para el usuario durante la retención. Si el usuario ya la tenía, renueva el TTL.
func (s *Asientos) Bloquear(ctx context.Context, ref, asiento string, usuarioID int64) error {
	if usuarioID == 0 || !domain.FormatoAsiento.MatchString(asiento) {
		return domain.Invalido("Butaca o usuario inválidos.")
	}
	fid, err := s.Resolver(ctx, ref)
	if err != nil {
		return err
	}
	evento, vendidos, _, err := s.catalogo.ContextoReserva(ctx, fid, []string{asiento}, usuarioID)
	if err != nil {
		return errorDeFuncion(err)
	}
	if err := s.turnos.ExigirTurno(ctx, evento, usuarioID); err != nil {
		return err
	}
	if vendidos > 0 {
		return domain.Conflicto("La butaca " + asiento + " ya no está disponible.")
	}

	switch r, err := s.candados.Bloquear(ctx, fid, asiento, usuarioID, s.retencion, domain.MaxAsientosPorCompra); {
	case err != nil:
		return err
	case r == 0:
		return domain.Conflicto("La butaca " + asiento + " acaba de ser tomada por otro cliente.")
	case r < 0:
		return domain.Conflicto(fmt.Sprintf("Puedes bloquear hasta %d butacas a la vez.", domain.MaxAsientosPorCompra))
	case r == 2:
		return nil // renovación de un bloqueo propio: los demás clientes ya la ven bloqueada
	}
	s.turnos.ConfirmarTurno(ctx, evento, usuarioID)
	s.publicar(ctx, fid, ref, domain.AsientoBloqueado, []string{asiento})
	return nil
}

// Desbloquear suelta butacas del usuario (sólo las que siguen a su nombre).
func (s *Asientos) Desbloquear(ctx context.Context, ref string, asientos []string, usuarioID int64) error {
	if usuarioID == 0 || len(asientos) == 0 || len(asientos) > domain.MaxAsientosPorCompra {
		return domain.Invalido("Solicitud inválida.")
	}
	fid, err := s.Resolver(ctx, ref)
	if err != nil {
		return err
	}
	liberadas, err := s.candados.LiberarBloqueos(ctx, fid, asientos, usuarioID)
	if err != nil {
		return err
	}
	s.publicar(ctx, fid, ref, domain.AsientoLiberado, liberadas)
	return nil
}

// Suscribir registra un stream de cambios de una función. Hay que llamar a la función devuelta al terminar.
func (s *Asientos) Suscribir(ctx context.Context, funcionID int) (<-chan string, func(), error) {
	return s.candados.Suscribir(ctx, funcionID)
}

// publicar difunde un cambio; ref puede ser el id numérico o el código, se normaliza al código público.
func (s *Asientos) publicar(ctx context.Context, fid int, ref string, tipo domain.TipoEventoAsiento, asientos []string) {
	if len(asientos) == 0 {
		return
	}
	codigo := ref
	if c, err := s.catalogo.CodigoFuncion(ctx, fid); err == nil {
		codigo = c
	}
	if err := s.candados.Publicar(ctx, fid, domain.EventoAsientos{Tipo: tipo, Funcion: codigo, Asientos: asientos}); err != nil {
		log.Printf("tiempo real: no se pudo publicar %s en la función %d: %v", tipo, fid, err)
	}
}

// EscucharExpiraciones convierte los vencimientos de Redis (TTL de bloqueos y reservas) en eventos "liberado".
// Corre hasta que ctx se cancela.
func (s *Asientos) EscucharExpiraciones(ctx context.Context) {
	s.candados.Expiraciones(ctx, func(fid int, asiento string) {
		s.publicar(ctx, fid, "", domain.AsientoLiberado, []string{asiento})
	})
}
