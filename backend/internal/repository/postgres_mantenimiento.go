package repository

import (
	"context"
	"fmt"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Migrar aplica al arrancar los cambios de esquema y de datos que una base ya creada (p. ej. Supabase) no trae
// de init.sql. Todo es idempotente: correrlo en cada arranque no duplica nada.
func (p *Postgres) Migrar(ctx context.Context) error {
	return pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, candadoProgramacion); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, esquemaFueraServicio); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id, activo)
			VALUES ('Venta en taquilla', $1, 'sin-acceso', 2, FALSE) ON CONFLICT (correo) DO NOTHING`, domain.CorreoTaquilla); err != nil {
			return err
		}
		// Los clientes que se registraron con el bono anterior reciben el bono vigente una sola vez
		// (la referencia única del movimiento evita acreditarlo dos veces).
		_, err := tx.Exec(ctx, `
			WITH nuevos AS (
				INSERT INTO movimientos_credito (usuario_id, tipo, monto, referencia)
				SELECT m.usuario_id, 'recarga', $1::int, $2::text || m.usuario_id
				FROM movimientos_credito m
				WHERE m.tipo = 'recarga' AND m.referencia LIKE 'bono-bienvenida:%' AND m.monto < $1::int
				ON CONFLICT DO NOTHING
				RETURNING usuario_id, monto)
			UPDATE cuentas_credito c SET saldo = c.saldo + n.monto FROM nuevos n WHERE c.usuario_id = n.usuario_id`,
			domain.BonoBienvenida, fmt.Sprintf("bono-%d:", domain.BonoBienvenida))
		return err
	})
}

// esquemaFueraServicio es la tabla de butacas que el personal retira de la venta (dañadas o en mantenimiento).
// Aplica a todas las funciones de la sala mientras la butaca siga marcada.
const esquemaFueraServicio = `
	CREATE TABLE IF NOT EXISTS butacas_fuera_servicio (
	  sala_id   INT NOT NULL REFERENCES salas(id),
	  asiento   TEXT NOT NULL,
	  motivo    TEXT NOT NULL CHECK (motivo IN ('danada','fuera-servicio')),
	  marcada_por TEXT NOT NULL DEFAULT '',
	  desde     TIMESTAMPTZ NOT NULL DEFAULT now(),
	  PRIMARY KEY (sala_id, asiento)
	);
	ALTER TABLE butacas_fuera_servicio ENABLE ROW LEVEL SECURITY;`

// ButacasFueraServicio lista las butacas retiradas de una sala.
func (p *Postgres) ButacasFueraServicio(ctx context.Context, sala string) ([]domain.ButacaFueraServicio, error) {
	var existe bool
	if err := p.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM salas WHERE codigo = $1)`, sala).Scan(&existe); err != nil {
		return nil, err
	}
	if !existe {
		return nil, domain.ErrNoExiste
	}
	rows, err := p.db.Query(ctx, `
		SELECT b.asiento, b.motivo, b.marcada_por, b.desde FROM butacas_fuera_servicio b JOIN salas s ON s.id = b.sala_id
		WHERE s.codigo = $1 ORDER BY b.asiento`, sala)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.ButacaFueraServicio])
}

// MarcarFueraServicio retira una butaca de la venta (o cambia su motivo). Devuelve domain.ErrNoExiste si la sala
// no existe o la butaca no cabe en sus filas y columnas.
func (p *Postgres) MarcarFueraServicio(ctx context.Context, sala, asiento, motivo, por string) error {
	tag, err := p.db.Exec(ctx, `
		INSERT INTO butacas_fuera_servicio (sala_id, asiento, motivo, marcada_por)
		SELECT s.id, $2, $3, $4 FROM salas s
		WHERE s.codigo = $1 AND ascii(left($2, 1)) - 64 <= s.filas AND substring($2 FROM 3)::int BETWEEN 1 AND s.columnas
		ON CONFLICT (sala_id, asiento) DO UPDATE SET motivo = EXCLUDED.motivo, marcada_por = EXCLUDED.marcada_por, desde = now()`,
		sala, asiento, motivo, por)
	if err == nil && tag.RowsAffected() == 0 {
		err = domain.ErrNoExiste
	}
	return err
}

// HabilitarButaca devuelve una butaca a la venta.
func (p *Postgres) HabilitarButaca(ctx context.Context, sala, asiento string) error {
	tag, err := p.db.Exec(ctx, `
		DELETE FROM butacas_fuera_servicio b USING salas s WHERE s.id = b.sala_id AND s.codigo = $1 AND b.asiento = $2`, sala, asiento)
	if err == nil && tag.RowsAffected() == 0 {
		err = domain.ErrNoExiste
	}
	return err
}

// FuncionesVigentesDeSala devuelve los ids y códigos de las funciones por venir de una sala (para avisar a los mapas abiertos).
func (p *Postgres) FuncionesVigentesDeSala(ctx context.Context, sala string) (map[int]string, error) {
	rows, err := p.db.Query(ctx, `
		SELECT f.id, f.codigo FROM funciones f JOIN salas s ON s.id = f.sala_id WHERE s.codigo = $1 AND f.fin > now()`, sala)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var id int
		var codigo string
		if err := rows.Scan(&id, &codigo); err != nil {
			return nil, err
		}
		out[id] = codigo
	}
	return out, rows.Err()
}

// ocuparPreestrenos llena con ventas de taquilla los preestrenos por venir que aún no las tienen, hasta dejar
// domain.LibresPreestreno butacas libres. Las butacas que quedan libres salen de un orden fijo por función
// (md5 del código y la butaca), así quedan repartidas por la sala y no cambian entre arranques.
func ocuparPreestrenos(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO boletos (token, funcion_id, asiento, usuario_id, precio, reserva_id)
		SELECT 'TQ-' || upper(substr(md5(random()::text || x.asiento), 1, 12)), x.funcion_id, x.asiento,
		       (SELECT id FROM usuarios WHERE correo = $2), 0, 'taquilla:' || x.codigo
		FROM (
			SELECT f.id AS funcion_id, f.codigo, a.asiento,
			       row_number() OVER (PARTITION BY f.id ORDER BY md5(f.codigo || a.asiento)) AS n,
			       count(*) OVER (PARTITION BY f.id) AS libres
			FROM funciones f
			JOIN salas s ON s.id = f.sala_id
			CROSS JOIN LATERAL (
				SELECT chr(64 + r) || '-' || lpad(c::text, 2, '0') AS asiento
				FROM generate_series(1, s.filas) r, generate_series(1, s.columnas) c) a
			WHERE f.evento IS NOT NULL AND f.inicio > now() AND f.estado = 'programada'
			  AND NOT EXISTS (SELECT 1 FROM boletos b WHERE b.funcion_id = f.id AND b.reserva_id = 'taquilla:' || f.codigo)
			  AND NOT EXISTS (SELECT 1 FROM boletos b WHERE b.funcion_id = f.id AND b.asiento = a.asiento AND b.estado IN ('valido','utilizado'))
			  AND NOT EXISTS (SELECT 1 FROM butacas_fuera_servicio o WHERE o.sala_id = s.id AND o.asiento = a.asiento)
		) x
		WHERE x.n <= x.libres - $1`, domain.LibresPreestreno, domain.CorreoTaquilla)
	if err != nil {
		return fmt.Errorf("ocupar preestreno: %w", err)
	}
	return nil
}
