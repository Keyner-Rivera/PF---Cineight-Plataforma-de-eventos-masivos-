package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Bloqueo transaccional de butacas en PostgreSQL (RF-54, RNF-36, RNF-37).
//
// Cada butaca elegida, reservada o pagada pasa por una transacción que primero bloquea la fila de su función con
// SELECT … FOR UPDATE: dos clientes que operan sobre la misma función se atienden uno detrás de otro, así que la
// comprobación "¿sigue libre?" y la escritura ocurren sin que nadie más se cuele en medio. La tabla `retenciones`
// (una fila por butaca, con clave primaria función + butaca) es el registro de quién tiene cada butaca y hasta cuándo:
//   - selección: reserva_id NULL, la crea cada clic en el mapa;
//   - reserva:   reserva_id = folio, la crea "Comprar boletos" (todas las butacas o ninguna);
//   - pago:      se borra en la misma transacción que emite los boletos y debita los créditos.
// Redis conserva los candados para el mapa en tiempo real, pero quien decide es PostgreSQL.

const esquemaRetenciones = `
	CREATE TABLE IF NOT EXISTS retenciones (
	  funcion_id  INT NOT NULL REFERENCES funciones(id),
	  asiento     TEXT NOT NULL,
	  usuario_id  BIGINT NOT NULL REFERENCES usuarios(id),
	  reserva_id  TEXT,
	  expira      TIMESTAMPTZ NOT NULL,
	  PRIMARY KEY (funcion_id, asiento)
	);
	CREATE INDEX IF NOT EXISTS ix_retenciones_reserva ON retenciones (reserva_id) WHERE reserva_id IS NOT NULL;
	CREATE INDEX IF NOT EXISTS ix_retenciones_expira ON retenciones (expira);
	ALTER TABLE retenciones ENABLE ROW LEVEL SECURITY;`

// funcionBloqueada son los datos de una función leídos con su fila bloqueada (FOR UPDATE) hasta el fin de la transacción.
type funcionBloqueada struct {
	id              int
	precio          int
	evento          *string
	abierta         bool
	salaID          int
	filas, columnas int
}

func bloquearFuncion(ctx context.Context, tx pgx.Tx, funcionID int) (funcionBloqueada, error) {
	f := funcionBloqueada{id: funcionID}
	err := tx.QueryRow(ctx, `
		SELECT f.precio, f.evento, f.inicio > now() AND f.estado = 'programada', s.id, s.filas, s.columnas
		FROM funciones f JOIN salas s ON s.id = f.sala_id WHERE f.id = $1 FOR UPDATE OF f`, funcionID).
		Scan(&f.precio, &f.evento, &f.abierta, &f.salaID, &f.filas, &f.columnas)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, domain.ErrNoExiste
	}
	if err == nil && !f.abierta {
		err = domain.ErrFuncionIniciada
	}
	return f, err
}

// verificarButacas comprueba, con la función bloqueada, que las butacas existan en la sala, no estén vendidas ni
// fuera de servicio. Es la disponibilidad real según la base de datos, no lo que diga el cliente.
func verificarButacas(ctx context.Context, tx pgx.Tx, f funcionBloqueada, asientos []string) error {
	for _, a := range asientos {
		fila := int(a[0]-'A') + 1
		col, err := strconv.Atoi(a[2:])
		if err != nil || fila < 1 || fila > f.filas || col < 1 || col > f.columnas {
			return domain.ErrAsientoInexistente
		}
	}
	var vendidas, fuera int
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM boletos WHERE funcion_id = $1 AND asiento = ANY($2) AND estado IN ('valido','utilizado')),
		       (SELECT count(*) FROM butacas_fuera_servicio WHERE sala_id = $3 AND asiento = ANY($2))`,
		f.id, asientos, f.salaID).Scan(&vendidas, &fuera); err != nil {
		return err
	}
	if vendidas+fuera > 0 {
		return domain.ErrAsientoVendido
	}
	return nil
}

// tomar inserta o renueva la retención de una butaca. Sólo la toma si está libre, si ya era del mismo cliente en
// selección, o si la retención anterior venció. Devuelve false si la tiene otro.
func tomar(ctx context.Context, tx pgx.Tx, funcionID int, asiento string, usuarioID int64, reservaID *string, ttl time.Duration) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO retenciones (funcion_id, asiento, usuario_id, reserva_id, expira) VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		ON CONFLICT (funcion_id, asiento) DO UPDATE SET usuario_id = EXCLUDED.usuario_id, reserva_id = EXCLUDED.reserva_id, expira = EXCLUDED.expira
		WHERE (retenciones.usuario_id = EXCLUDED.usuario_id AND retenciones.reserva_id IS NULL) OR retenciones.expira <= now()`,
		funcionID, asiento, usuarioID, reservaID, ttl.Seconds())
	return err == nil && tag.RowsAffected() == 1, err
}

// RetenerSeleccion retiene en la base una butaca elegida en el mapa (máximo `maximo` por cliente y función).
func (p *Postgres) RetenerSeleccion(ctx context.Context, funcionID int, asiento string, usuarioID int64, ttl time.Duration, maximo int) error {
	return pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		f, err := bloquearFuncion(ctx, tx, funcionID)
		if err != nil {
			return err
		}
		if err := verificarButacas(ctx, tx, f, []string{asiento}); err != nil {
			return err
		}
		var propias int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM retenciones WHERE funcion_id = $1 AND usuario_id = $2 AND asiento <> $3 AND expira > now()`,
			funcionID, usuarioID, asiento).Scan(&propias); err != nil {
			return err
		}
		if propias >= maximo {
			return domain.ErrLimiteButacas
		}
		ok, err := tomar(ctx, tx, funcionID, asiento, usuarioID, nil, ttl)
		if err == nil && !ok {
			err = domain.ErrButacaOcupada
		}
		return err
	})
}

// SoltarSeleccion libera butacas que el cliente eligió pero no reservó.
func (p *Postgres) SoltarSeleccion(ctx context.Context, funcionID int, asientos []string, usuarioID int64) error {
	_, err := p.db.Exec(ctx, `DELETE FROM retenciones WHERE funcion_id = $1 AND asiento = ANY($2) AND usuario_id = $3 AND reserva_id IS NULL`,
		funcionID, asientos, usuarioID)
	return err
}

// RetenerReserva pasa las butacas a la reserva: todas o ninguna, con la disponibilidad y el límite del preestreno
// verificados con la función bloqueada. Devuelve el precio unitario vigente de la función.
func (p *Postgres) RetenerReserva(ctx context.Context, r domain.Reserva, ttl time.Duration, maxEvento int) (precio int, err error) {
	err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		f, err := bloquearFuncion(ctx, tx, r.FuncionID)
		if err != nil {
			return err
		}
		if err := verificarButacas(ctx, tx, f, r.Asientos); err != nil {
			return err
		}
		if f.evento != nil {
			// Preestreno: boletos ya comprados + butacas en otras reservas vigentes + éstas.
			var propios int
			if err := tx.QueryRow(ctx, `
				SELECT (SELECT count(*) FROM boletos WHERE funcion_id = $1 AND usuario_id = $2 AND estado IN ('valido','utilizado'))
				     + (SELECT count(*) FROM retenciones WHERE funcion_id = $1 AND usuario_id = $2 AND reserva_id IS NOT NULL
				          AND reserva_id <> $3 AND expira > now())`, r.FuncionID, r.UsuarioID, r.ID).Scan(&propios); err != nil {
				return err
			}
			if propios+len(r.Asientos) > maxEvento {
				return domain.ErrLimiteEvento
			}
		}
		for _, a := range r.Asientos {
			ok, err := tomar(ctx, tx, r.FuncionID, a, r.UsuarioID, &r.ID, ttl)
			if err != nil {
				return err
			}
			if !ok {
				return domain.ErrButacaOcupada // la transacción se revierte: ninguna butaca queda retenida
			}
		}
		precio = f.precio
		return nil
	})
	return precio, err
}

// LiberarReserva borra las retenciones de una reserva del usuario (cancelación).
func (p *Postgres) LiberarReserva(ctx context.Context, reservaID string, usuarioID int64) error {
	_, err := p.db.Exec(ctx, `DELETE FROM retenciones WHERE reserva_id = $1 AND usuario_id = $2`, reservaID, usuarioID)
	return err
}

// LimpiarRetencionesVencidas borra las retenciones cuyo tiempo terminó y devuelve qué butacas quedaron libres.
func (p *Postgres) LimpiarRetencionesVencidas(ctx context.Context) (map[int][]string, error) {
	rows, err := p.db.Query(ctx, `DELETE FROM retenciones WHERE expira <= now() RETURNING funcion_id, asiento`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]string{}
	for rows.Next() {
		var fid int
		var a string
		if err := rows.Scan(&fid, &a); err != nil {
			return nil, err
		}
		out[fid] = append(out[fid], a)
	}
	return out, rows.Err()
}
