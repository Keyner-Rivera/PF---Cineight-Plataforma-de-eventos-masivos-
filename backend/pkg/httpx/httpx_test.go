package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestIPDelCliente(t *testing.T) {
	casos := []struct {
		nombre, cf, xff, quiere string
		proxy                   bool
	}{
		{"sin proxy se ignoran las cabeceras", "9.9.9.9", "8.8.8.8", "192.0.2.1", false},
		{"detrás de Cloudflare manda CF-Connecting-IP", "203.0.113.7", "1.1.1.1, 10.0.0.5", "203.0.113.7", true},
		{"sin Cloudflare, la última de X-Forwarded-For", "", "1.1.1.1, 10.0.0.5", "10.0.0.5", true},
		{"sin cabeceras, la conexión", "", "", "192.0.2.1", true},
	}
	defer func() { ConfiarProxy = false }()
	for _, c := range casos {
		ConfiarProxy = c.proxy
		r := httptest.NewRequest("GET", "/", nil)
		if c.cf != "" {
			r.Header.Set("CF-Connecting-IP", c.cf)
		}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := IP(r); got != c.quiere {
			t.Errorf("%s: %s, se esperaba %s", c.nombre, got, c.quiere)
		}
	}
}
