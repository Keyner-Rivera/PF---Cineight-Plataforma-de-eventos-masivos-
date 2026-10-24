package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/redis/go-redis/v9"
)

// Cola virtual de un preestreno (RF-36, RF-51). Por evento:
//
//	cola:{evento}           ZSET de quienes esperan, puntaje = momento de llegada (orden FIFO).
//	turnos:{evento}         ZSET de quienes tienen turno, puntaje = momento en que vence.
//	latidos:{evento}        ZSET de quienes esperan, puntaje = última vez que consultaron su posición.
//	turnos-inicio:{evento}  HASH usuario → momento en que recibió el turno.
//
// Quien deja de consultar (cerró la página) pierde su lugar; los turnos vencen solos y liberan el cupo.
// Un turno empieza con un minuto de gracia: si en ese tiempo el cliente no bloquea ninguna butaca, pasa al siguiente;
// si bloquea, ConfirmarTurno lo extiende hasta completar la duración normal desde que lo recibió.
func clavesCola(evento string) []string {
	return []string{"cola:" + evento, "turnos:" + evento, "latidos:" + evento, "turnos-inicio:" + evento}
}

// avanzar: depura la cola, admite por orden de llegada hasta llenar el cupo y devuelve el estado del usuario.
// ARGV: ahora (ms), cupo, gracia del turno (ms), inactividad máxima (ms), usuario.
// Devuelve {1, vencimiento del turno, en cola, confirmado 0/1} · {0, posición (1 = siguiente), en cola, 0}
// · {-1, 0, en cola, 0} si no está.
var scriptAvanzarCola = redis.NewScript(`
local ahora = tonumber(ARGV[1])
for _, u in ipairs(redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ahora)) do
  redis.call('ZREM', KEYS[2], u)
  redis.call('HDEL', KEYS[4], u)
end
for _, u in ipairs(redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', ahora - tonumber(ARGV[4]))) do
  redis.call('ZREM', KEYS[1], u)
  redis.call('ZREM', KEYS[3], u)
end
if redis.call('ZSCORE', KEYS[1], ARGV[5]) then redis.call('ZADD', KEYS[3], ahora, ARGV[5]) end
local libres = tonumber(ARGV[2]) - redis.call('ZCARD', KEYS[2])
if libres > 0 then
  local siguientes = redis.call('ZPOPMIN', KEYS[1], libres)
  for i = 1, #siguientes, 2 do
    redis.call('ZADD', KEYS[2], ahora + tonumber(ARGV[3]), siguientes[i])
    redis.call('HSET', KEYS[4], siguientes[i], ahora)
    redis.call('ZREM', KEYS[3], siguientes[i])
  end
end
local total = redis.call('ZCARD', KEYS[1])
local vence = redis.call('ZSCORE', KEYS[2], ARGV[5])
if vence then
  local inicio = tonumber(redis.call('HGET', KEYS[4], ARGV[5]) or vence)
  local confirmado = 0
  if tonumber(vence) - inicio > tonumber(ARGV[3]) then confirmado = 1 end
  return {1, tonumber(vence), total, confirmado}
end
local rango = redis.call('ZRANK', KEYS[1], ARGV[5])
if rango then return {0, rango + 1, total, 0} end
return {-1, 0, total, 0}`)

// confirmar: el cliente con turno bloqueó una butaca; el turno vence a la duración completa desde que lo recibió.
// ARGV: ahora (ms), duración del turno (ms), usuario. Devuelve el nuevo vencimiento (0 si no tenía turno vigente).
var scriptConfirmarTurno = redis.NewScript(`
local vence = redis.call('ZSCORE', KEYS[2], ARGV[3])
if not vence or tonumber(vence) <= tonumber(ARGV[1]) then return 0 end
local inicio = redis.call('HGET', KEYS[4], ARGV[3])
if not inicio then return tonumber(vence) end
local completo = tonumber(inicio) + tonumber(ARGV[2])
if completo > tonumber(vence) then
  redis.call('ZADD', KEYS[2], 'XX', completo, ARGV[3])
  return completo
end
return tonumber(vence)`)

// entrar: agrega al usuario al final de la cola si no está esperando ni tiene turno. ARGV: ahora (ms), usuario.
var scriptEntrarCola = redis.NewScript(`
local vence = redis.call('ZSCORE', KEYS[2], ARGV[2])
if vence and tonumber(vence) > tonumber(ARGV[1]) then return 0 end
redis.call('ZADD', KEYS[1], 'NX', ARGV[1], ARGV[2])
redis.call('ZADD', KEYS[3], ARGV[1], ARGV[2])
return 1`)

// EntrarCola agrega al usuario a la cola del evento. Si ya estaba, conserva su posición original (RF-51).
func (r *Redis) EntrarCola(ctx context.Context, evento, usuario string, ahora time.Time) error {
	return falla(scriptEntrarCola.Run(ctx, r.rdb, clavesCola(evento), ahora.UnixMilli(), usuario).Err())
}

// AvanzarCola admite a los siguientes de la fila mientras haya cupo (cada uno con un turno de gracia) y devuelve
// el estado del usuario.
func (r *Redis) AvanzarCola(ctx context.Context, evento, usuario string, ahora time.Time, cupo int64, gracia, inactividad time.Duration) (domain.EstadoCola, error) {
	res, err := scriptAvanzarCola.Run(ctx, r.rdb, clavesCola(evento), ahora.UnixMilli(), cupo, gracia.Milliseconds(), inactividad.Milliseconds(), usuario).Int64Slice()
	if err != nil {
		return domain.EstadoCola{}, falla(err)
	}
	e := domain.EstadoCola{EnCola: res[2]}
	switch res[0] {
	case 1:
		e.Turno = true
		e.Vence = time.UnixMilli(res[1])
		e.Confirmado = res[3] == 1
	case 0:
		e.EnFila = true
		e.Posicion = res[1]
	}
	return e, nil
}

// ConfirmarTurno extiende el turno del usuario a su duración completa (bloqueó una butaca dentro de la gracia).
func (r *Redis) ConfirmarTurno(ctx context.Context, evento, usuario string, ahora time.Time, turno time.Duration) error {
	return falla(scriptConfirmarTurno.Run(ctx, r.rdb, clavesCola(evento), ahora.UnixMilli(), turno.Milliseconds(), usuario).Err())
}

// SalirCola quita al usuario de la fila y libera su turno (abandonó o ya compró).
func (r *Redis) SalirCola(ctx context.Context, evento, usuario string) error {
	pipe := r.rdb.TxPipeline()
	claves := clavesCola(evento)
	for _, k := range claves[:3] {
		pipe.ZRem(ctx, k, usuario)
	}
	pipe.HDel(ctx, claves[3], usuario)
	_, err := pipe.Exec(ctx)
	return falla(err)
}

// TurnoActivo indica si el usuario tiene un turno vigente para el evento.
func (r *Redis) TurnoActivo(ctx context.Context, evento, usuario string, ahora time.Time) (bool, error) {
	vence, err := r.rdb.ZScore(ctx, "turnos:"+evento, usuario).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, falla(err)
	}
	return int64(vence) > ahora.UnixMilli(), nil
}

// Colas devuelve cuántos esperan en cada cola virtual activa (métricas).
func (r *Redis) Colas(ctx context.Context) map[string]int64 {
	out := map[string]int64{}
	iter := r.rdb.Scan(ctx, 0, "cola:*", 100).Iterator()
	for iter.Next(ctx) {
		out[strings.TrimPrefix(iter.Val(), "cola:")], _ = r.rdb.ZCard(ctx, iter.Val()).Result()
	}
	return out
}

// Contar incrementa el contador de solicitudes de una IP en la ventana del minuto actual (RF-53).
func (r *Redis) Contar(ctx context.Context, grupo, ip string) (int64, error) {
	clave := fmt.Sprintf("rl:%s:%s:%d", grupo, ip, time.Now().Unix()/60)
	n, err := r.rdb.Incr(ctx, clave).Result()
	if err == nil && n == 1 {
		r.rdb.Expire(ctx, clave, time.Minute)
	}
	return n, err
}
