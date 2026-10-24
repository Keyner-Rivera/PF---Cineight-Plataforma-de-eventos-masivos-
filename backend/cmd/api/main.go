// API de Cineight (prototipo). Demuestra las piezas críticas de la ERS:
//   - Bloqueo de butacas en tiempo real con SET NX EX en Redis y difusión por SSE (RF-07, RF-54)
//   - Pago con créditos en una transacción ACID con SELECT ... FOR UPDATE (RF-63, RNF-37)
//   - Índice único parcial en boletos como última barrera contra la sobreventa (RNF-36)
//   - Idempotencia del pago (RF-23) y de los premios de trivia (RF-64)
//   - Cola virtual con ZSET (RF-36, RF-51), rate limiting y reCAPTCHA v3 (RF-53)
//
// Estructura:
//
//	cmd/api              arranque y cableado de dependencias (este archivo)
//	internal/config      configuración desde variables de entorno
//	internal/domain      entidades, reglas fijas y errores de negocio
//	internal/repository  acceso a PostgreSQL y Redis
//	internal/services    lógica de negocio
//	internal/handlers    capa HTTP (rutas, JSON, SSE)
//	pkg/                 utilidades reutilizables sin dependencias del dominio
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // la imagen alpine no trae la base de zonas horarias

	"github.com/cineight/backend/internal/config"
	"github.com/cineight/backend/internal/handlers"
	"github.com/cineight/backend/internal/repository"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
	"github.com/cineight/backend/pkg/recaptcha"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Cargar()
	if err != nil {
		log.Fatalf("configuración incompleta: %v", err)
	}
	ctx, detener := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer detener()

	db, err := repository.AbrirPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		log.Fatalf("PostgreSQL: %v", err)
	}
	defer db.Close()
	// REDIS_URL (rediss:// con TLS en Upstash, Render, Railway…) o REDIS_ADDR en local.
	opciones := &redis.Options{Addr: cfg.RedisAddr}
	if cfg.RedisURL != "" {
		if opciones, err = redis.ParseURL(cfg.RedisURL); err != nil {
			log.Fatalf("REDIS_URL inválida: %v", err)
		}
	}
	// Tiempos cortos: si Redis cae, las solicitudes fallan rápido con 503 en lugar de quedarse colgadas.
	opciones.DialTimeout, opciones.ReadTimeout, opciones.WriteTimeout, opciones.MaxRetries = 2*time.Second, time.Second, time.Second, 1
	// Pool acotado: los Redis administrados limitan las conexiones (Render Key Value gratuito: 50). Con REDIS_POOL
	// más las 2 suscripciones (difusión de butacas y vencimientos) la API no pasa de ese límite.
	opciones.PoolSize = cfg.RedisPool
	rdb := redis.NewClient(opciones)
	defer rdb.Close()
	httpx.ConfiarProxy = cfg.ConfiarProxy
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("Redis no responde al arrancar (%v): las reservas responderán 503 hasta que vuelva", err)
	}

	// Repositorios
	pg := repository.NuevoPostgres(db)
	rd := repository.NuevoRedis(rdb)

	// Servicios
	seguridad := services.NuevaSeguridad(recaptcha.Nuevo(cfg.RecaptchaKey, cfg.RecaptchaMin), rd, rd, pg)
	cola := services.NuevaCola(rd, seguridad, pg, rd, cfg.ColaCapacidad, cfg.ColaTurno)
	asientos := services.NuevoAsientos(pg, rd, cola, cfg.Retencion)
	cartelera := services.NuevaCartelera(pg, pg, rd, cfg.CacheCartelera)
	srv := handlers.Servicios{
		Asientos:  asientos,
		Reservas:  services.NuevoReservas(asientos, pg, rd, pg, cfg.Retencion),
		Cuentas:   services.NuevoCuentas(pg, pg, pg, seguridad),
		Cola:      cola,
		Panel:     services.NuevoPanel(pg, rd, rd),
		Seguridad: seguridad,
		Cartelera: cartelera,
		Butacas:   services.NuevoButacas(pg, asientos, pg, cartelera.InvalidarCartelera),
	}

	// Una sola suscripción de Redis reparte los cambios de butacas entre todos los mapas abiertos de esta instancia.
	go rd.IniciarDifusion(ctx)
	// Los vencimientos de Redis (TTL) se difunden como "liberado" a los mapas abiertos.
	go asientos.EscucharExpiraciones(ctx)
	// Hoy y los próximos días siempre tienen funciones, aunque la base de datos se haya creado hace días.
	go services.NuevaProgramacion(pg, cartelera.InvalidarCartelera).Mantener(ctx)

	origenes := httpx.ListaOrigenes(cfg.OrigenesCORS)
	servidor := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           handlers.NuevoRouter(srv, handlers.Limites{Sensible: cfg.RateLimit, Interactivo: cfg.RateLimitAlto}, origenes),
		ReadHeaderTimeout: 10 * time.Second,
		// Sin WriteTimeout: las conexiones SSE permanecen abiertas. Sus contextos derivan de ctx,
		// así que al recibir SIGTERM se cierran y Shutdown no queda esperándolas.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	go func() {
		<-ctx.Done()
		apagar, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		_ = servidor.Shutdown(apagar)
	}()

	log.Printf("Cineight API escuchando en %s · CORS: %v · reCAPTCHA: %v · cola virtual: %d turnos de %s · pool PostgreSQL: %d–%d · caché cartelera: %s",
		servidor.Addr, cfg.OrigenesCORS, cfg.RecaptchaKey != "", cfg.ColaCapacidad, cfg.ColaTurno, cfg.DBMinConns, cfg.DBMaxConns, cfg.CacheCartelera)
	if err := servidor.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
