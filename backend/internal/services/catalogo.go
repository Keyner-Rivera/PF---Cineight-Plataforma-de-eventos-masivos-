package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cineight/backend/internal/domain"
	"golang.org/x/text/unicode/norm"
)

// Programador permite a agentes y administradores crear películas y programar funciones.
type Programador interface {
	CrearPelicula(ctx context.Context, p domain.PeliculaAlta, funciones []domain.NuevaFuncion) (string, []string, error)
	ProgramarFuncion(ctx context.Context, pelicula string, f domain.NuevaFuncion) (string, error)
	PeliculasPersonalizadas(ctx context.Context) ([]domain.Pelicula, error)
	Poster(ctx context.Context, slug string) ([]byte, string, error)
}

// Cartelera gestiona la cartelera vigente y el catálogo editable.
type Cartelera struct {
	repo     Programador
	catalogo Catalogo
	cache    Cache
	ttl      time.Duration

	mu    sync.Mutex // también evita que, al vencer la copia, muchas solicitudes consulten la base de datos a la vez
	local CarteleraJSON
	vence time.Time
}

// CarteleraJSON es la cartelera ya serializada, tal cual y comprimida con gzip (~10 veces más chica).
type CarteleraJSON struct {
	Plano []byte
	Gzip  []byte
}

// NuevaCartelera crea el servicio. ttl es la vida de la cartelera en caché (0 = siempre desde la base de datos).
func NuevaCartelera(r Programador, c Catalogo, cache Cache, ttl time.Duration) *Cartelera {
	return &Cartelera{repo: r, catalogo: c, cache: cache, ttl: ttl}
}

// claveCartelera es la entrada de caché de GET /api/funciones.
const claveCartelera = "funciones"

// Funciones devuelve la cartelera vigente ya decodificada (ver FuncionesJSON).
func (s *Cartelera) Funciones(ctx context.Context) ([]domain.Funcion, error) {
	c, err := s.FuncionesJSON(ctx)
	if err != nil {
		return nil, err
	}
	var lista []domain.Funcion
	return lista, json.Unmarshal(c.Plano, &lista)
}

// FuncionesJSON devuelve la cartelera vigente lista para enviarse. Es la consulta más pedida (portada, fichas, panel)
// y pesa decenas de KB, así que no se vuelve a serializar en cada solicitud: durante ttl se sirve una copia en memoria
// (con su versión gzip), que a su vez sale de la caché de Redis compartida entre instancias o de PostgreSQL.
// Se invalida al programar funciones, crear películas o retirar butacas. Las butacas libres de cada función pueden
// ir hasta ttl segundos atrasadas: el mapa de butacas usa su propia consulta en tiempo real.
func (s *Cartelera) FuncionesJSON(ctx context.Context) (CarteleraJSON, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ttl > 0 && time.Now().Before(s.vence) {
		return s.local, nil
	}
	var datos []byte
	if s.ttl > 0 {
		datos, _ = s.cache.LeerCache(ctx, claveCartelera)
	}
	if datos == nil {
		lista, err := s.catalogo.Funciones(ctx)
		if err != nil {
			return CarteleraJSON{}, err
		}
		if lista == nil {
			lista = []domain.Funcion{}
		}
		if datos, err = json.Marshal(lista); err != nil {
			return CarteleraJSON{}, err
		}
		if s.ttl > 0 {
			s.cache.GuardarCache(ctx, claveCartelera, datos, s.ttl)
		}
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(datos)
	_ = w.Close()
	s.local, s.vence = CarteleraJSON{Plano: datos, Gzip: gz.Bytes()}, time.Now().Add(s.ttl)
	return s.local, nil
}

// InvalidarCartelera descarta la cartelera en caché para que la próxima consulta lea la base de datos.
func (s *Cartelera) InvalidarCartelera(ctx context.Context) {
	s.mu.Lock()
	s.vence = time.Time{}
	s.mu.Unlock()
	s.cache.BorrarCache(ctx, claveCartelera)
}

const (
	maxPosterBytes   = 2 << 20 // 2 MB ya decodificado
	maxFuncionesAlta = 40
)

var (
	tiposPoster = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
	idYouTube   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

// SolicitudPelicula es el cuerpo de POST /api/peliculas.
type SolicitudPelicula struct {
	domain.Pelicula
	Poster    string                `json:"poster"` // data URL (data:image/jpeg;base64,…)
	Funciones []domain.NuevaFuncion `json:"funciones"`
	CreadaPor string                `json:"creada_por"`
}

// Crear valida la ficha, la imagen y las funciones, y guarda todo junto.
func (s *Cartelera) Crear(ctx context.Context, sol SolicitudPelicula) (string, []string, error) {
	p, err := normalizarFicha(sol.Pelicula, len(sol.Funciones))
	if err != nil {
		return "", nil, err
	}
	poster, tipo, err := decodificarPoster(sol.Poster)
	if err != nil {
		return "", nil, err
	}
	for i := range sol.Funciones {
		if err := validarFuncion(&sol.Funciones[i]); err != nil {
			return "", nil, err
		}
	}
	p.Slug = slugDe(p.Titulo)
	slug, codigos, err := s.repo.CrearPelicula(ctx, domain.PeliculaAlta{Pelicula: p, Poster: poster, PosterTipo: tipo, CreadaPor: sol.CreadaPor}, sol.Funciones)
	if err == nil {
		s.InvalidarCartelera(ctx)
	}
	return slug, codigos, traducirTraslape(err)
}

// Programar agrega una función a una película existente.
func (s *Cartelera) Programar(ctx context.Context, pelicula string, f domain.NuevaFuncion) (string, error) {
	if err := validarFuncion(&f); err != nil {
		return "", err
	}
	codigo, err := s.repo.ProgramarFuncion(ctx, pelicula, f)
	if errors.Is(err, domain.ErrNoExiste) {
		return "", domain.NoEncontrado("La película no existe en la base de datos.")
	}
	if err == nil {
		s.InvalidarCartelera(ctx)
	}
	return codigo, traducirTraslape(err)
}

// Personalizadas lista las películas creadas desde el panel.
func (s *Cartelera) Personalizadas(ctx context.Context) ([]domain.Pelicula, error) {
	return s.repo.PeliculasPersonalizadas(ctx)
}

// Poster devuelve la imagen de una película.
func (s *Cartelera) Poster(ctx context.Context, slug string) ([]byte, string, error) {
	b, t, err := s.repo.Poster(ctx, slug)
	if err != nil {
		return nil, "", domain.NoEncontrado("Póster no disponible.")
	}
	return b, t, nil
}

// normalizarFicha limpia los textos de la ficha y comprueba sus reglas (RF-05, RF-67).
func normalizarFicha(p domain.Pelicula, nFunciones int) (domain.Pelicula, error) {
	p.Titulo = strings.TrimSpace(p.Titulo)
	p.TituloOriginal = strings.TrimSpace(p.TituloOriginal)
	if p.TituloOriginal == "" {
		p.TituloOriginal = p.Titulo
	}
	if p.Trailer != nil && *p.Trailer == "" {
		p.Trailer = nil
	}
	if p.Categoria != "proximamente" {
		p.EstrenoEnDias = nil
	}
	if msg := reglaIncumplida(p, nFunciones); msg != "" {
		return p, domain.Invalido(msg)
	}
	return p, nil
}

// reglaIncumplida devuelve el mensaje de la primera regla de la ficha que no se cumple ("" si todas se cumplen).
func reglaIncumplida(p domain.Pelicula, nFunciones int) string {
	titulo := len([]rune(p.Titulo))
	switch {
	case titulo < 2 || titulo > 120:
		return "El título debe tener entre 2 y 120 caracteres."
	case !domain.Categorias[p.Categoria]:
		return "Categoría inválida."
	case !domain.Clasificaciones[p.Clasificacion]:
		return "Clasificación inválida."
	case p.Duracion < 30 || p.Duracion > 300:
		return "La duración debe estar entre 30 y 300 minutos."
	case len([]rune(strings.TrimSpace(p.Sinopsis))) < 20:
		return "Escribe una sinopsis de al menos 20 caracteres."
	case p.Trailer != nil && !idYouTube.MatchString(*p.Trailer):
		return "El tráiler debe ser un enlace o id de YouTube válido."
	case p.Categoria == "proximamente" && nFunciones > 0:
		return "Las películas «Próximamente» aún no tienen funciones a la venta."
	case nFunciones > maxFuncionesAlta:
		return fmt.Sprintf("Máximo %d funciones por alta.", maxFuncionesAlta)
	}
	return ""
}

func validarFuncion(f *domain.NuevaFuncion) error {
	f.Formato, f.Idioma = strings.TrimSpace(f.Formato), strings.TrimSpace(f.Idioma)
	switch {
	case f.Sala == "":
		return domain.Invalido("Cada función necesita una sala.")
	case f.Inicio.Before(time.Now()):
		return domain.Invalido("No se pueden programar funciones en el pasado.")
	case f.Precio <= 0 || f.Precio > 500:
		return domain.Invalido("El precio debe estar entre 1 y 500 créditos.")
	}
	if f.Formato == "" {
		f.Formato = "2D"
	}
	if f.Idioma == "" {
		f.Idioma = "Subtitulada"
	}
	return nil
}

// zonaGuatemala para los mensajes (UTC-6 sin horario de verano; respaldo fijo si falta la base de zonas).
var zonaGuatemala = func() *time.Location {
	if z, err := time.LoadLocation("America/Guatemala"); err == nil {
		return z
	}
	return time.FixedZone("GT", -6*3600)
}()

// traducirTraslape convierte el traslape del repositorio en un 409 legible.
func traducirTraslape(err error) error {
	var t *domain.ErrTraslape
	if errors.As(err, &t) {
		return domain.Conflicto(fmt.Sprintf("Traslape en %s con «%s» (%s–%s, incluye %d min de limpieza).",
			t.Sala, t.Titulo, t.Inicio.In(zonaGuatemala).Format("02/01 15:04"), t.Fin.In(zonaGuatemala).Format("15:04"), domain.LimpiezaMin))
	}
	return err
}

// decodificarPoster valida el data URL: tipo permitido (comprobado con los bytes reales) y tamaño máximo.
func decodificarPoster(dataURL string) ([]byte, string, error) {
	cabecera, b64, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(cabecera, "data:") || !strings.HasSuffix(cabecera, ";base64") {
		return nil, "", domain.Invalido("Sube una imagen para el póster.")
	}
	datos, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", domain.Invalido("La imagen del póster está dañada.")
	}
	if len(datos) > maxPosterBytes {
		return nil, "", domain.Invalido("El póster no puede pesar más de 2 MB.")
	}
	tipo := http.DetectContentType(datos)
	if !tiposPoster[tipo] {
		return nil, "", domain.Invalido("El póster debe ser JPG, PNG o WebP.")
	}
	return datos, tipo, nil
}

// slugDe convierte "Furiosa: De la saga Mad Max" en "furiosa-de-la-saga-mad-max".
func slugDe(titulo string) string {
	var b strings.Builder
	guion := false
	for _, r := range norm.NFD.String(strings.ToLower(titulo)) {
		switch {
		case unicode.Is(unicode.Mn, r): // acentos
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if r < 128 {
				b.WriteRune(r)
				guion = false
			}
		case !guion && b.Len() > 0:
			b.WriteByte('-')
			guion = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if len(s) > 60 {
		s = strings.TrimSuffix(s[:60], "-")
	}
	if s == "" {
		s = "pelicula"
	}
	return s
}
