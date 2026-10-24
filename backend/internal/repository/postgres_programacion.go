package repository

import (
	"context"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ProgramarDias completa la cartelera de los días indicados (medianoche local de cada uno) según el plan.
// Una sala que ya tiene alguna función ese día no se toca: así no se duplica lo que creó init.sql ni lo que
// programó un agente. Al final, si no queda ningún preestreno por delante, la última función de la película del preestreno
// de mañana en su sala pasa a ser el nuevo preestreno con cola virtual, y cada preestreno nuevo recibe sus ventas de
// taquilla (ver ocuparPreestrenos). Devuelve cuántas funciones creó.
func (p *Postgres) ProgramarDias(ctx context.Context, dias []time.Time, plan []domain.PlanSala) (int, error) {
	creadas := 0
	err := pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, candadoProgramacion); err != nil {
			return err
		}
		duraciones := map[string]int{}
		rows, err := tx.Query(ctx, `SELECT slug, duracion_min FROM peliculas`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var slug string
			var minutos int
			if err := rows.Scan(&slug, &minutos); err != nil {
				rows.Close()
				return err
			}
			duraciones[slug] = minutos
		}
		rows.Close()
		duracion := func(slug string) int { return duraciones[slug] }

		for _, dia := range dias {
			paridad := int(dia.Unix()/86400) % 2
			for _, ps := range plan {
				var ocupada bool
				if err := tx.QueryRow(ctx, `
					SELECT EXISTS (SELECT 1 FROM funciones f JOIN salas s ON s.id = f.sala_id
					               WHERE s.codigo = $1 AND f.inicio >= $2 AND f.inicio < $3)`,
					ps.Sala, dia, dia.AddDate(0, 0, 1)).Scan(&ocupada); err != nil {
					return err
				}
				if ocupada {
					continue
				}
				for _, fp := range domain.FuncionesDelDia(ps, paridad, duracion) {
					if err := insertarProgramada(ctx, tx, ps.Sala, dia.Add(time.Duration(fp.Minuto)*time.Minute), fp); err != nil {
						return err
					}
					creadas++
				}
			}
		}

		if len(dias) > 1 {
			_, err = tx.Exec(ctx, `
				UPDATE funciones SET evento = $1, precio = $2
				WHERE id = (SELECT f.id FROM funciones f
				            JOIN peliculas pe ON pe.id = f.pelicula_id JOIN salas s ON s.id = f.sala_id
				            WHERE pe.slug = $3 AND s.codigo = $4 AND f.inicio >= $5 AND f.inicio < $6 AND f.estado = 'programada'
				            ORDER BY f.inicio DESC LIMIT 1)
				  AND NOT EXISTS (SELECT 1 FROM funciones WHERE evento = $1 AND inicio > now())`,
				domain.EventoPreestreno, domain.PrecioPreestreno, domain.PeliculaPreestreno, domain.SalaPreestreno, dias[1], dias[1].AddDate(0, 0, 1))
			if err != nil {
				return err
			}
		}
		return ocuparPreestrenos(ctx, tx)
	})
	return creadas, err
}

// insertarProgramada crea una función del plan con el siguiente código fN (todas las butacas libres).
func insertarProgramada(ctx context.Context, tx pgx.Tx, sala string, inicio time.Time, fp domain.FuncionPlan) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO funciones (codigo, pelicula_id, sala_id, inicio, fin, formato, idioma, precio)
		SELECT 'f' || (SELECT coalesce(max(substring(codigo FROM 2)::int), 0) + 1 FROM funciones WHERE codigo ~ '^f[0-9]+$'),
		       pe.id, s.id, $3::timestamptz, $3::timestamptz + make_interval(mins => pe.duracion_min + $7::int), $4, $5, $6
		FROM peliculas pe, salas s WHERE pe.slug = $1 AND s.codigo = $2`, fp.Pelicula, sala, inicio, fp.Formato, fp.Idioma, fp.Precio, domain.LimpiezaMin)
	return err
}
