package repository

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// difusor reparte los cambios de butacas entre los streams SSE de esta instancia con UNA sola suscripción a Redis
// (PSUBSCRIBE asientos:*). Antes cada mapa abierto tenía su propia conexión de Pub/Sub, y un Redis administrado con
// límite de conexiones (Render Key Value gratuito: 50) dejaba de aceptar clientes con unas decenas de mapas abiertos.
type difusor struct {
	mu    sync.Mutex
	subs  map[int]map[chan string]struct{} // función → canales de los streams abiertos
	listo chan struct{}                    // se cierra cuando Redis confirma la suscripción
}

// bufferStream es cuántos cambios puede acumular un stream lento antes de cerrarse. Al cerrarse, el navegador
// reconecta y recibe la foto completa, así que no se pierde ningún estado.
const bufferStream = 64

// IniciarDifusion abre la suscripción compartida. go-redis la restablece sola si Redis se reinicia.
// Corre hasta que ctx se cancela.
func (r *Redis) IniciarDifusion(ctx context.Context) {
	sub := r.rdb.PSubscribe(ctx, prefijoCanal+"*")
	defer sub.Close()
	confirmada := false
	for m := range sub.ChannelWithSubscriptions(redis.WithChannelHealthCheckInterval(15 * time.Second)) {
		switch msg := m.(type) {
		case *redis.Subscription:
			if !confirmada && msg.Kind == "psubscribe" {
				confirmada = true
				close(r.dif.listo)
			}
		case *redis.Message:
			if id, err := strconv.Atoi(strings.TrimPrefix(msg.Channel, prefijoCanal)); err == nil {
				r.dif.repartir(id, msg.Payload)
			}
		}
	}
}

// repartir entrega un cambio a los streams de la función sin bloquearse por uno lento.
func (d *difusor) repartir(funcionID int, payload string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subs[funcionID] {
		select {
		case ch <- payload:
		default:
			delete(d.subs[funcionID], ch)
			close(ch)
			log.Printf("tiempo real: stream lento de la función %d cerrado (reconectará con la foto completa)", funcionID)
		}
	}
}

// Suscribir registra un stream de la función. Devuelve el canal de cambios (JSON de domain.EventoAsientos) y la
// función que lo da de baja; el canal se cierra si el stream se queda atrás. Si Redis no responde, devuelve
// domain.ErrNoDisponible.
func (r *Redis) Suscribir(ctx context.Context, funcionID int) (<-chan string, func(), error) {
	espera, cancelar := context.WithTimeout(ctx, 3*time.Second)
	defer cancelar()
	select {
	case <-r.dif.listo:
	case <-espera.Done():
		return nil, nil, falla(errors.New("la suscripción de tiempo real no está lista"))
	}
	if err := r.rdb.Ping(espera).Err(); err != nil {
		return nil, nil, falla(err)
	}
	ch := make(chan string, bufferStream)
	d := r.dif
	d.mu.Lock()
	if d.subs[funcionID] == nil {
		d.subs[funcionID] = map[chan string]struct{}{}
	}
	d.subs[funcionID][ch] = struct{}{}
	d.mu.Unlock()
	baja := func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if _, ok := d.subs[funcionID][ch]; ok {
			delete(d.subs[funcionID], ch)
			close(ch)
		}
		if len(d.subs[funcionID]) == 0 {
			delete(d.subs, funcionID)
		}
	}
	return ch, baja, nil
}
