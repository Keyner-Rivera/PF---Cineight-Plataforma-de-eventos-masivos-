package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/pkg/token"
	"github.com/jackc/pgx/v5"
)

const columnasBoleto = `b.token, f.codigo, b.asiento, b.precio, b.estado, b.fecha_venta, p.slug, s.codigo, f.inicio, f.formato, f.idioma, b.reserva_id
	FROM boletos b JOIN funciones f ON f.id = b.funcion_id JOIN peliculas p ON p.id = f.pelicula_id JOIN salas s ON s.id = f.sala_id`

// BoletosDeReserva devuelve los boletos que un pago emitió al usuario (vacío si la reserva aún no se pagó o es de otro).
func (p *Postgres) BoletosDeReserva(ctx context.Context, reservaID string, usuarioID int64) []domain.Boleto {
	rows, err := p.db.Query(ctx, `SELECT `+columnasBoleto+` WHERE b.reserva_id = $1 AND b.usuario_id = $2 ORDER BY b.asiento`, reservaID, usuarioID)
	if err != nil {
		return nil
	}
	lista, _ := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Boleto])
	return lista
}

// RegistrarVenta cobra una reserva y emite sus boletos en UNA transacción (RF-63, RNF-37), sin confiar en nada de lo
// que mande el cliente:
//  1. Bloquea la fila de la función (FOR UPDATE): los pagos y reservas de esa función se atienden de uno en uno.
//  2. La función debe seguir abierta y la reserva vigente EN LA BASE: sus retenciones existen, son del usuario, no
//     vencieron y corresponden exactamente a las butacas (si expiró, se rechaza aunque el cliente insista).
//  3. Vuelve a verificar que las butacas existan, no estén vendidas ni fuera de servicio, y el límite del preestreno.
//  4. Recalcula el total con la tarifa vigente de la función en la base (el cliente nunca envía precios).
//  5. Bloquea la cuenta del usuario (FOR UPDATE: no hay doble gasto), debita, emite los boletos y borra las retenciones.
//
// Si algo falla, la transacción se revierte completa. El índice único parcial de boletos es la última barrera (RNF-36).
func (p *Postgres) RegistrarVenta(ctx context.Context, r domain.Reserva, maxEvento int) (saldoFinal, total int, err error) {
	err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		f, err := bloquearFuncion(ctx, tx, r.FuncionID)
		if err != nil {
			return err
		}
		var retenidas int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM retenciones
			WHERE reserva_id = $1 AND usuario_id = $2 AND funcion_id = $3 AND asiento = ANY($4) AND expira > now()`,
			r.ID, r.UsuarioID, r.FuncionID, r.Asientos).Scan(&retenidas); err != nil {
			return err
		}
		if retenidas != len(r.Asientos) {
			return domain.ErrReservaVencida
		}
		if err := verificarButacas(ctx, tx, f, r.Asientos); err != nil {
			return err
		}
		if f.evento != nil {
			var propios int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM boletos WHERE funcion_id = $1 AND usuario_id = $2 AND estado IN ('valido','utilizado')`,
				r.FuncionID, r.UsuarioID).Scan(&propios); err != nil {
				return err
			}
			if propios+len(r.Asientos) > maxEvento {
				return domain.ErrLimiteEvento
			}
		}
		total = f.precio * len(r.Asientos)

		var saldo int
		if err := tx.QueryRow(ctx, `SELECT saldo FROM cuentas_credito WHERE usuario_id = $1 FOR UPDATE`, r.UsuarioID).Scan(&saldo); err != nil {
			return err
		}
		if saldo < total {
			return domain.ErrSaldoInsuficiente
		}
		saldoFinal = saldo - total
		if _, err := tx.Exec(ctx, `UPDATE cuentas_credito SET saldo = saldo - $1 WHERE usuario_id = $2`, total, r.UsuarioID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO movimientos_credito (usuario_id, tipo, monto, referencia) VALUES ($1, 'debito', $2, $3)`,
			r.UsuarioID, -total, r.ID); err != nil {
			return err
		}
		for _, as := range r.Asientos {
			if _, err := tx.Exec(ctx, `INSERT INTO boletos (token, funcion_id, asiento, usuario_id, precio, reserva_id) VALUES ($1,$2,$3,$4,$5,$6)`,
				"CE-"+token.Nuevo(12), r.FuncionID, as, r.UsuarioID, f.precio, r.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM retenciones WHERE reserva_id = $1`, r.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO bitacora_auditoria (usuario_id, accion, entidad, valor_nuevo) VALUES ($1, 'VENTA', 'boletos', $2)`,
			r.UsuarioID, map[string]any{"reserva": r.ID, "asientos": r.Asientos, "precio_unitario": f.precio, "total": total})
		return err
	})
	if EsViolacionUnica(err) {
		err = domain.ErrAsientoVendido
	}
	return saldoFinal, total, err
}

// ValidarBoleto marca un boleto como utilizado. El UPDATE condicional es atómico: un boleto no entra dos veces.
func (p *Postgres) ValidarBoleto(ctx context.Context, tok string) (domain.Validacion, error) {
	var b domain.Boleto
	var titular string
	err := p.db.QueryRow(ctx, `
		UPDATE boletos b SET estado = 'utilizado' FROM funciones f, usuarios u, peliculas p, salas s
		WHERE b.token = $1 AND b.estado = 'valido' AND f.id = b.funcion_id AND u.id = b.usuario_id AND p.id = f.pelicula_id AND s.id = f.sala_id
		RETURNING b.token, f.codigo, b.asiento, b.precio, b.estado, b.fecha_venta, p.slug, s.codigo, f.inicio, f.formato, f.idioma, b.reserva_id, u.correo`, tok).
		Scan(&b.Token, &b.Funcion, &b.Asiento, &b.Precio, &b.Estado, &b.Fecha, &b.Pelicula, &b.Sala, &b.Inicio, &b.Formato, &b.Idioma, &b.Reserva, &titular)
	if err == nil {
		return domain.Validacion{OK: true, Motivo: "Acceso permitido", Boleto: &b, Titular: titular}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Validacion{}, err
	}

	// No se pudo usar: averiguar por qué (inexistente, ya utilizado, anulado…).
	consulta := `SELECT ` + strings.Replace(columnasBoleto, "FROM boletos b", ", u.correo FROM boletos b JOIN usuarios u ON u.id = b.usuario_id", 1) + ` WHERE b.token = $1`
	err = p.db.QueryRow(ctx, consulta, tok).Scan(&b.Token, &b.Funcion, &b.Asiento, &b.Precio, &b.Estado, &b.Fecha, &b.Pelicula, &b.Sala, &b.Inicio, &b.Formato, &b.Idioma, &b.Reserva, &titular)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Validacion{OK: false, Motivo: "Boleto inexistente"}, nil
	}
	if err != nil {
		return domain.Validacion{}, err
	}
	motivo := "Boleto " + b.Estado
	if b.Estado == "utilizado" {
		motivo = "Boleto ya utilizado"
	}
	return domain.Validacion{OK: false, Motivo: motivo, Boleto: &b, Titular: titular}, nil
}

// Comprobante reúne los datos de una compra (folio = id de la reserva pagada). Con usuarioID > 0 sólo la devuelve si es
// de ese usuario; con 0 (verificación pública) la devuelve sin el correo del titular. domain.ErrNoExiste si no hay.
func (p *Postgres) Comprobante(ctx context.Context, folio string, usuarioID int64) (domain.Comprobante, error) {
	columnas := strings.Replace(columnasBoleto, "FROM boletos b", ", u.nombre, u.correo, p.titulo, s.nombre FROM boletos b JOIN usuarios u ON u.id = b.usuario_id", 1)
	rows, err := p.db.Query(ctx, `SELECT `+columnas+`
		WHERE b.reserva_id = $1 AND ($2 = 0 OR b.usuario_id = $2) AND b.reserva_id NOT LIKE 'taquilla:%' ORDER BY b.asiento`, folio, usuarioID)
	if err != nil {
		return domain.Comprobante{}, err
	}
	defer rows.Close()
	var c domain.Comprobante
	for rows.Next() {
		var b domain.Boleto
		var nombre, correo, titulo, sala string
		if err := rows.Scan(&b.Token, &b.Funcion, &b.Asiento, &b.Precio, &b.Estado, &b.Fecha, &b.Pelicula, &b.Sala, &b.Inicio, &b.Formato, &b.Idioma, &b.Reserva,
			&nombre, &correo, &titulo, &sala); err != nil {
			return c, err
		}
		if c.Folio == "" {
			c = domain.Comprobante{Folio: folio, Fecha: b.Fecha, Cliente: nombre, Correo: correo, Pelicula: titulo, Funcion: b.Funcion, Sala: sala,
				Inicio: b.Inicio, Formato: b.Formato, Idioma: b.Idioma, PrecioUnitario: b.Precio}
		}
		c.Asientos = append(c.Asientos, b.Asiento)
		c.Total += b.Precio
		c.Boletos = append(c.Boletos, b)
	}
	if err := rows.Err(); err != nil {
		return c, err
	}
	if c.Folio == "" {
		return c, domain.ErrNoExiste
	}
	return c, nil
}
