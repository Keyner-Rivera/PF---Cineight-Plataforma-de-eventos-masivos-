package repository

import (
	"context"
	"fmt"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// VincularCliente crea (si no existe) al usuario y su cuenta de créditos con el bono de bienvenida.
// Devuelve su id y saldo actual.
func (p *Postgres) VincularCliente(ctx context.Context, correo, nombre string) (id int64, saldo int, err error) {
	err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id) VALUES ($1, $2, 'pendiente-jwt', 1)
			ON CONFLICT (correo) DO UPDATE SET correo = EXCLUDED.correo RETURNING id`, nombre, correo).Scan(&id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO cuentas_credito (usuario_id, saldo) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, domain.BonoBienvenida)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if _, err := tx.Exec(ctx, `INSERT INTO movimientos_credito (usuario_id, tipo, monto, referencia) VALUES ($1, 'recarga', $2, $3)`,
				id, domain.BonoBienvenida, fmt.Sprintf("bono-bienvenida:%d", id)); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `SELECT saldo FROM cuentas_credito WHERE usuario_id = $1`, id).Scan(&saldo)
	})
	return
}

// Cuenta devuelve saldo, últimos movimientos y boletos de un usuario.
func (p *Postgres) Cuenta(ctx context.Context, usuarioID int64) (domain.Cuenta, error) {
	var c domain.Cuenta
	if err := p.db.QueryRow(ctx, `SELECT saldo FROM cuentas_credito WHERE usuario_id = $1`, usuarioID).Scan(&c.Saldo); err != nil {
		return c, domain.ErrNoExiste
	}
	rows, err := p.db.Query(ctx, `SELECT id, tipo, monto, referencia, fecha FROM movimientos_credito WHERE usuario_id = $1 ORDER BY fecha DESC LIMIT 50`, usuarioID)
	if err != nil {
		return c, err
	}
	if c.Movimientos, err = pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Movimiento]); err != nil {
		return c, err
	}
	rows, err = p.db.Query(ctx, `SELECT `+columnasBoleto+` WHERE b.usuario_id = $1 ORDER BY b.fecha_venta DESC, b.asiento`, usuarioID)
	if err != nil {
		return c, err
	}
	c.Boletos, err = pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Boleto])
	return c, err
}

// AcreditarTrivia suma el premio una sola vez por partida: la referencia única evita cobrarla dos veces (RF-64).
func (p *Postgres) AcreditarTrivia(ctx context.Context, usuarioID int64, partida string, monto int) (acreditado bool, saldo int, err error) {
	err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		if monto > 0 {
			tag, err := tx.Exec(ctx, `INSERT INTO movimientos_credito (usuario_id, tipo, monto, referencia) VALUES ($1, 'recarga', $2, $3) ON CONFLICT DO NOTHING`,
				usuarioID, monto, "trivia:"+partida)
			if err != nil {
				return err
			}
			if acreditado = tag.RowsAffected() == 1; acreditado {
				if _, err := tx.Exec(ctx, `UPDATE cuentas_credito SET saldo = saldo + $1 WHERE usuario_id = $2`, monto, usuarioID); err != nil {
					return err
				}
			}
		}
		return tx.QueryRow(ctx, `SELECT saldo FROM cuentas_credito WHERE usuario_id = $1`, usuarioID).Scan(&saldo)
	})
	return
}

// Metricas calcula los indicadores del panel a partir de PostgreSQL. Las ventas de taquilla que llenan el preestreno
// para pruebas no cuentan como ventas del día (sí ocupan butacas en la ocupación por función).
func (p *Postgres) Metricas(ctx context.Context) (domain.Metricas, error) {
	m := domain.Metricas{Funciones: []domain.OcupacionFuncion{}}
	_ = p.db.QueryRow(ctx, `SELECT count(*), coalesce(sum(precio), 0) FROM boletos WHERE fecha_venta >= date_trunc('day', now()) AND estado IN ('valido','utilizado') AND reserva_id NOT LIKE 'taquilla:%'`).
		Scan(&m.VendidosHoy, &m.IngresosHoy)
	_ = p.db.QueryRow(ctx, `SELECT coalesce(sum(monto) FILTER (WHERE referencia LIKE 'trivia:%'), 0), coalesce(-sum(monto) FILTER (WHERE tipo = 'debito'), 0) FROM movimientos_credito`).
		Scan(&m.CreditosTrivia, &m.CreditosCanjeados)
	_ = p.db.QueryRow(ctx, `SELECT count(*) FROM usuarios WHERE activo`).Scan(&m.Cuentas)

	rows, err := p.db.Query(ctx, `
		SELECT f.codigo, count(b.id), s.aforo, coalesce(sum(b.precio), 0)
		FROM funciones f JOIN salas s ON s.id = f.sala_id
		LEFT JOIN boletos b ON b.funcion_id = f.id AND b.estado IN ('valido','utilizado')
		WHERE f.fin > now() GROUP BY f.id, s.aforo ORDER BY f.inicio`)
	if err != nil {
		return m, err
	}
	m.Funciones, err = pgx.CollectRows(rows, pgx.RowToStructByPos[domain.OcupacionFuncion])
	return m, err
}
