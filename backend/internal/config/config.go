// Package config lee la configuración de la API desde variables de entorno.
// Las credenciales nunca tienen valores por defecto en el código: si faltan, la API no arranca.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config agrupa todos los parámetros de arranque.
type Config struct {
	Puerto         string
	DatabaseURL    string
	DBMaxConns     int32 // conexiones máximas del pool de PostgreSQL
	DBMinConns     int32 // conexiones que el pool mantiene abiertas aunque no haya tráfico
	RedisAddr      string
	RedisURL       string // redis:// o rediss:// (TLS); si está definida tiene prioridad sobre RedisAddr
	RedisPool      int    // conexiones máximas del pool de Redis (sin contar las 2 suscripciones de Pub/Sub)
	OrigenesCORS   string
	ConfiarProxy   bool          // detrás del proxy de un hosting: la IP del cliente sale de X-Forwarded-For
	RateLimit      int64         // solicitudes por minuto por IP en rutas sensibles (reservas, cola)
	RateLimitAlto  int64         // límite para rutas interactivas (bloqueo de butacas, captcha)
	ColaCapacidad  int64         // clientes que eligen butacas a la vez desde la cola virtual
	ColaTurno      time.Duration // tiempo de cada turno de la cola virtual
	Retencion      time.Duration // vida del bloqueo de una butaca y de una reserva (RF-07)
	CacheCartelera time.Duration // vida de la cartelera en la caché de Redis (0 = sin caché)
	RecaptchaKey   string        // clave secreta de reCAPTCHA v3; vacía = verificación desactivada
	RecaptchaMin   float64       // puntaje mínimo aceptado (0.0–1.0)
}

// Cargar construye la configuración a partir del entorno y valida lo obligatorio.
func Cargar() (Config, error) {
	dsn, err := urlBaseDatos()
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Puerto:        texto("PORT", "8080"),
		DatabaseURL:   dsn,
		DBMaxConns:    int32(entero("DB_MAX_CONNS", 25)),
		DBMinConns:    int32(entero("DB_MIN_CONNS", 5)),
		RedisAddr:     texto("REDIS_ADDR", "localhost:6379"),
		RedisURL:      os.Getenv("REDIS_URL"),
		RedisPool:     entero("REDIS_POOL", 20),
		OrigenesCORS:  texto("CORS_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000"),
		ConfiarProxy:  os.Getenv("TRUST_PROXY") == "true" || os.Getenv("TRUST_PROXY") == "1",
		RateLimit:     int64(entero("RATE_LIMIT", 5)),
		RateLimitAlto: int64(entero("RATE_LIMIT_INTERACTIVO", 120)),
		// Pocos turnos simultáneos: con un puñado de clientes ya se ve la fila avanzar (para producción, subirlo).
		ColaCapacidad:  int64(entero("COLA_CAPACIDAD", 5)),
		ColaTurno:      time.Duration(entero("COLA_TURNO_MINUTOS", 3)) * time.Minute,
		Retencion:      time.Duration(entero("RETENCION_SEGUNDOS", 300)) * time.Second,
		CacheCartelera: time.Duration(entero("CACHE_CARTELERA_SEGUNDOS", 15)) * time.Second,
		RecaptchaKey:   os.Getenv("RECAPTCHA_SECRET"),
		RecaptchaMin:   decimal("RECAPTCHA_MIN_SCORE", 0.5),
	}
	switch {
	case c.DBMaxConns < 1:
		return c, errors.New("DB_MAX_CONNS debe ser al menos 1")
	case c.DBMinConns < 0 || c.DBMinConns > c.DBMaxConns:
		return c, fmt.Errorf("DB_MIN_CONNS debe estar entre 0 y DB_MAX_CONNS (%d)", c.DBMaxConns)
	}
	return c, nil
}

// urlBaseDatos usa DATABASE_URL (Render, Supabase, docker-compose) o la arma con DB_HOST, DB_PORT, DB_USER,
// DB_PASSWORD, DB_NAME y DB_SSLMODE. La contraseña se codifica para que caracteres como @ o # no rompan la URL.
func urlBaseDatos() (string, error) {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u, nil
	}
	host, usuario, clave := os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")
	if host == "" || usuario == "" || clave == "" {
		return "", errors.New("falta la conexión a PostgreSQL: define DATABASE_URL, o DB_HOST, DB_USER y DB_PASSWORD (y opcionalmente DB_PORT, DB_NAME, DB_SSLMODE)")
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(usuario, clave),
		Host:     net.JoinHostPort(host, texto("DB_PORT", "5432")),
		Path:     "/" + texto("DB_NAME", "postgres"),
		RawQuery: url.Values{"sslmode": {texto("DB_SSLMODE", "require")}}.Encode(),
	}
	return u.String(), nil
}

func texto(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func entero(k string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return n
	}
	return def
}

func decimal(k string, def float64) float64 {
	if n, err := strconv.ParseFloat(os.Getenv(k), 64); err == nil {
		return n
	}
	return def
}
