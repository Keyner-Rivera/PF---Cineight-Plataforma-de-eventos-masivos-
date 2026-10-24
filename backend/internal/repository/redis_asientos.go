package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/redis/go-redis/v9"
)

// Claves en Redis:
//
//	asiento:{funcion}:{butaca}   valor "u:{usuario}" (bloqueo desde el mapa) o "R-…" (reserva). TTL = retención.
//	bloqueos:{funcion}:{usuario} SET con las butacas que el usuario bloqueó (para limitar cuántas puede tomar).
//	reserva:{id}                 HASH con funcion_id, usuario_id y asientos.
//	asientos:{funcion}           canal Pub/Sub con los cambios de estado de las butacas.
const (
	prefijoAsiento = "asiento:"
	prefijoCanal   = "asientos:"
)

// Redis agrupa el estado efímero de la API.
type Redis struct {
	rdb *redis.Client
	dif *difusor // reparte el Pub/Sub de butacas entre los streams SSE (ver IniciarDifusion)
}

// NuevoRedis crea el repositorio sobre un cliente go-redis.
func NuevoRedis(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb, dif: &difusor{subs: map[int]map[chan string]struct{}{}, listo: make(chan struct{})}}
}

// Cliente expone el cliente subyacente (lo usa el oyente de expiraciones).
func (r *Redis) Cliente() *redis.Client { return r.rdb }

// falla marca un error de Redis como servicio no disponible (la API responde 503).
// redis.Nil no es una falla: significa que la clave no existe.
func falla(err error) error {
	if err == nil || errors.Is(err, redis.Nil) {
		return err
	}
	return fmt.Errorf("%w: %v", domain.ErrNoDisponible, err)
}

// ClaveAsiento devuelve la clave de una butaca.
func ClaveAsiento(funcionID int, asiento string) string {
	return fmt.Sprintf("%s%d:%s", prefijoAsiento, funcionID, asiento)
}

// ParsearClaveAsiento separa "asiento:12:C-04" en (12, "C-04").
func ParsearClaveAsiento(clave string) (funcionID int, asiento string, ok bool) {
	resto, ok := strings.CutPrefix(clave, prefijoAsiento)
	if !ok {
		return 0, "", false
	}
	fid, as, ok := strings.Cut(resto, ":")
	if !ok {
		return 0, "", false
	}
	n, err := strconv.Atoi(fid)
	return n, as, err == nil
}

// Propietario es el valor que identifica el bloqueo individual de un usuario.
func Propietario(usuarioID int64) string { return "u:" + strconv.FormatInt(usuarioID, 10) }

// Los scripts Lua se ejecutan de forma atómica en Redis: ningún otro cliente puede intercalar
// comandos entre la comprobación y la escritura, así que dos usuarios nunca obtienen la misma butaca.

// bloquear: KEYS[1]=butaca, KEYS[2]=set del usuario. ARGV: propietario, ttl, máximo, prefijo de butacas, etiqueta.
// Devuelve 1 si quedó bloqueada, 2 si se renovó un bloqueo propio, 0 si la tiene otro, -1 si el usuario alcanzó el máximo.
var scriptBloquear = redis.NewScript(`
local actual = redis.call('GET', KEYS[1])
if actual == ARGV[1] then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  return 2
end
if actual then return 0 end
local vivos = 0
for _, as in ipairs(redis.call('SMEMBERS', KEYS[2])) do
  if redis.call('GET', ARGV[4] .. as) == ARGV[1] then vivos = vivos + 1 else redis.call('SREM', KEYS[2], as) end
end
if vivos >= tonumber(ARGV[3]) then return -1 end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
redis.call('SADD', KEYS[2], ARGV[5])
redis.call('EXPIRE', KEYS[2], ARGV[2])
return 1`)

// liberarSi: borra cada clave sólo si su valor es ARGV[1]. Devuelve las claves liberadas.
var scriptLiberarSi = redis.NewScript(`
local liberadas = {}
for _, k in ipairs(KEYS) do
  if redis.call('GET', k) == ARGV[1] then
    redis.call('DEL', k)
    table.insert(liberadas, k)
  end
end
return liberadas`)

// retener: convierte las butacas en una reserva. Cada butaca debe estar libre o bloqueada por el mismo usuario.
// ARGV: id de reserva, propietario, ttl. Devuelve 0 si todo salió bien o la posición (1..n) de la primera butaca ocupada.
var scriptRetener = redis.NewScript(`
for i, k in ipairs(KEYS) do
  local v = redis.call('GET', k)
  if v and v ~= ARGV[2] then return i end
end
for _, k in ipairs(KEYS) do redis.call('SET', k, ARGV[1], 'EX', ARGV[3]) end
return 0`)

// Bloquear aplica el candado individual SET NX EX sobre una butaca.
func (r *Redis) Bloquear(ctx context.Context, funcionID int, asiento string, usuarioID int64, ttl time.Duration, maximo int) (int, error) {
	claves := []string{ClaveAsiento(funcionID, asiento), fmt.Sprintf("bloqueos:%d:%d", funcionID, usuarioID)}
	prefijo := fmt.Sprintf("%s%d:", prefijoAsiento, funcionID)
	n, err := scriptBloquear.Run(ctx, r.rdb, claves, Propietario(usuarioID), int(ttl.Seconds()), maximo, prefijo, asiento).Int()
	return n, falla(err)
}

// LiberarSi borra las butacas cuyo valor coincide con propietario y devuelve las etiquetas liberadas.
func (r *Redis) LiberarSi(ctx context.Context, funcionID int, asientos []string, propietario string) ([]string, error) {
	claves := make([]string, len(asientos))
	for i, as := range asientos {
		claves[i] = ClaveAsiento(funcionID, as)
	}
	res, err := scriptLiberarSi.Run(ctx, r.rdb, claves, propietario).StringSlice()
	if err != nil {
		return nil, falla(err)
	}
	liberadas := make([]string, 0, len(res))
	for _, k := range res {
		if _, as, ok := ParsearClaveAsiento(k); ok {
			liberadas = append(liberadas, as)
		}
	}
	return liberadas, nil
}

// Retener toma todas las butacas para la reserva o ninguna. Devuelve la butaca en conflicto ("" si no hubo).
func (r *Redis) Retener(ctx context.Context, res domain.Reserva, ttl time.Duration) (string, error) {
	claves := make([]string, len(res.Asientos))
	for i, as := range res.Asientos {
		claves[i] = ClaveAsiento(res.FuncionID, as)
	}
	n, err := scriptRetener.Run(ctx, r.rdb, claves, res.ID, Propietario(res.UsuarioID), int(ttl.Seconds())).Int()
	if err != nil || n == 0 {
		return "", falla(err)
	}
	return res.Asientos[n-1], nil
}

// Retenidos lista las butacas con candado vivo (bloqueadas o reservadas) de una función.
func (r *Redis) Retenidos(ctx context.Context, funcionID int) []string {
	prefijo := fmt.Sprintf("%s%d:", prefijoAsiento, funcionID)
	out := []string{}
	iter := r.rdb.Scan(ctx, 0, prefijo+"*", 200).Iterator()
	for iter.Next(ctx) {
		out = append(out, strings.TrimPrefix(iter.Val(), prefijo))
	}
	return out
}

// BloqueosDe devuelve las butacas que el usuario tiene bloqueadas desde el mapa en una función
// (sirve para restaurar su selección al recargar la página).
func (r *Redis) BloqueosDe(ctx context.Context, funcionID int, usuarioID int64) []string {
	out := []string{}
	duenio := Propietario(usuarioID)
	for _, as := range r.rdb.SMembers(ctx, fmt.Sprintf("bloqueos:%d:%d", funcionID, usuarioID)).Val() {
		if r.rdb.Get(ctx, ClaveAsiento(funcionID, as)).Val() == duenio {
			out = append(out, as)
		}
	}
	return out
}

// RetenidosPorFuncion agrupa los candados vivos de todas las funciones (un solo SCAN).
func (r *Redis) RetenidosPorFuncion(ctx context.Context) map[int][]string {
	out := map[int][]string{}
	iter := r.rdb.Scan(ctx, 0, prefijoAsiento+"*", 500).Iterator()
	for iter.Next(ctx) {
		if fid, as, ok := ParsearClaveAsiento(iter.Val()); ok {
			out[fid] = append(out[fid], as)
		}
	}
	return out
}

// TotalRetenidos cuenta los candados vivos de todas las funciones (métricas).
func (r *Redis) TotalRetenidos(ctx context.Context) int {
	n := 0
	iter := r.rdb.Scan(ctx, 0, prefijoAsiento+"*", 500).Iterator()
	for iter.Next(ctx) {
		n++
	}
	return n
}

// ───────── Reservas ─────────

// GuardarReserva registra los datos de la reserva con el mismo TTL que las butacas.
func (r *Redis) GuardarReserva(ctx context.Context, res domain.Reserva, ttl time.Duration) error {
	clave := "reserva:" + res.ID
	pipe := r.rdb.TxPipeline()
	pipe.HSet(ctx, clave, "funcion_id", res.FuncionID, "usuario_id", res.UsuarioID, "asientos", strings.Join(res.Asientos, ","))
	pipe.Expire(ctx, clave, ttl)
	_, err := pipe.Exec(ctx)
	return falla(err)
}

// Reserva lee una reserva vigente. Devuelve domain.ErrNoExiste si venció o no existe.
func (r *Redis) Reserva(ctx context.Context, id string) (domain.Reserva, error) {
	h, err := r.rdb.HGetAll(ctx, "reserva:"+id).Result()
	if err != nil {
		return domain.Reserva{}, falla(err)
	}
	if len(h) == 0 {
		return domain.Reserva{}, domain.ErrNoExiste
	}
	fid, _ := strconv.Atoi(h["funcion_id"])
	uid, _ := strconv.ParseInt(h["usuario_id"], 10, 64)
	return domain.Reserva{ID: id, FuncionID: fid, UsuarioID: uid, Asientos: strings.Split(h["asientos"], ",")}, nil
}

// BorrarReserva libera las butacas que siguen a nombre de la reserva y elimina su registro.
func (r *Redis) BorrarReserva(ctx context.Context, res domain.Reserva) ([]string, error) {
	liberadas, err := r.LiberarSi(ctx, res.FuncionID, res.Asientos, res.ID)
	r.rdb.Del(ctx, "reserva:"+res.ID)
	return liberadas, err
}

// ───────── Tiempo real (Pub/Sub) ─────────

// CanalFuncion es el canal Pub/Sub de una función.
func CanalFuncion(funcionID int) string { return prefijoCanal + strconv.Itoa(funcionID) }

// Publicar difunde un cambio de butacas a todas las instancias de la API (y de ahí a sus clientes SSE).
func (r *Redis) Publicar(ctx context.Context, funcionID int, ev domain.EventoAsientos) error {
	datos, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return falla(r.rdb.Publish(ctx, CanalFuncion(funcionID), datos).Err())
}

// LiberarBloqueos suelta las butacas que el usuario bloqueó desde el mapa.
func (r *Redis) LiberarBloqueos(ctx context.Context, funcionID int, asientos []string, usuarioID int64) ([]string, error) {
	return r.LiberarSi(ctx, funcionID, asientos, Propietario(usuarioID))
}

// Expiraciones llama a fn cada vez que vence el candado de una butaca (bloqueo o reserva).
// Activa notify-keyspace-events con la clase Ex; si el servidor no lo permite (Redis administrado),
// los vencimientos se reflejan cuando el cliente vuelve a pedir la ocupación.
// Bloquea hasta que ctx se cancela.
func (r *Redis) Expiraciones(ctx context.Context, fn func(funcionID int, asiento string)) {
	if err := r.rdb.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		log.Printf("tiempo real: no se pudo activar notify-keyspace-events: %v", err)
	}
	sub := r.rdb.PSubscribe(ctx, "__keyevent@*__:expired")
	defer sub.Close()
	// Si Redis cae, se reintenta con espera creciente (1 s, 2 s, 4 s… hasta 30 s) en lugar de insistir cada segundo.
	espera := time.Second
	for {
		msg, err := sub.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("tiempo real: oyente de expiraciones (reintento en %s): %v", espera, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(espera):
			}
			espera = min(espera*2, 30*time.Second)
			continue
		}
		espera = time.Second
		if fid, asiento, ok := ParsearClaveAsiento(msg.Payload); ok {
			fn(fid, asiento)
		}
	}
}
