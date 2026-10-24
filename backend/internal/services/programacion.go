package services

import (
	"context"
	"log"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// Programacion mantiene siempre programados hoy y los días siguientes (domain.DiasProgramados), así la cartelera
// nunca se queda sin funciones aunque la base de datos se haya creado hace días.
type Programacion struct {
	repo      PlanificadorDias
	zona      *time.Location
	alCambiar func(context.Context) // se llama cuando se crean funciones (p. ej. invalidar la caché de la cartelera)
}

// NuevaProgramacion crea el servicio. Usa la hora de Guatemala para decidir qué es "hoy".
func NuevaProgramacion(r PlanificadorDias, alCambiar func(context.Context)) *Programacion {
	zona, err := time.LoadLocation(domain.ZonaHoraria)
	if err != nil {
		zona = time.FixedZone("GT", -6*60*60)
	}
	return &Programacion{repo: r, zona: zona, alCambiar: alCambiar}
}

// Asegurar programa lo que falte desde hoy hasta domain.DiasProgramados - 1 días después.
func (s *Programacion) Asegurar(ctx context.Context, ahora time.Time) (int, error) {
	local := ahora.In(s.zona)
	hoy := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.zona)
	dias := make([]time.Time, domain.DiasProgramados)
	for i := range dias {
		dias[i] = hoy.AddDate(0, 0, i)
	}
	return s.repo.ProgramarDias(ctx, dias, domain.Plan)
}

// Mantener aplica las migraciones pendientes, ejecuta Asegurar al arrancar y luego cada hora, hasta que ctx se cancela.
// Si algo falla (p. ej. la base de datos todavía arranca, algo habitual en un hosting), reintenta a los 30 s.
func (s *Programacion) Mantener(ctx context.Context) {
	migrado := false
	for {
		espera := time.Hour
		var err error
		if !migrado {
			if err = s.repo.Migrar(ctx); err == nil {
				migrado = true
			}
		}
		if err != nil {
			log.Printf("migración al arrancar (reintento en 30 s): %v", err)
			espera = 30 * time.Second
		} else if n, err := s.Asegurar(ctx, time.Now()); err != nil {
			log.Printf("programación automática (reintento en 30 s): %v", err)
			espera = 30 * time.Second
		} else if n > 0 {
			log.Printf("programación automática: %d funciones nuevas", n)
			if s.alCambiar != nil {
				s.alCambiar(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
	}
}
