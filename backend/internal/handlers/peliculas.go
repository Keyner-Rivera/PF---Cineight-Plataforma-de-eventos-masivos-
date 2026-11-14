package handlers

import (
	"net/http"
	"strconv"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
)

// GET /api/peliculas → películas creadas desde el panel, con su ficha completa.
func (a *API) listarPeliculas(w http.ResponseWriter, r *http.Request) {
	lista, err := a.s.Cartelera.Personalizadas(r.Context())
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, lista)
}

// POST /api/peliculas  {ficha…, poster: dataURL, funciones: [{sala, inicio, formato, idioma, precio}]}
// Crea la película y sus funciones en una sola transacción (agente operativo o administrador).
func (a *API) crearPelicula(w http.ResponseWriter, r *http.Request) {
	var s services.SolicitudPelicula
	if err := httpx.LeerHasta(r, &s, 4<<20); err != nil {
		httpx.Error(w, 400, "Solicitud inválida o imagen demasiado grande (máx. 2 MB).")
		return
	}
	s.CreadaPor = sesion(r).Usuario.Correo
	slug, codigos, err := a.s.Cartelera.Crear(r.Context(), s)
	if err != nil {
		fallar(w, err)
		return
	}
	if codigos == nil {
		codigos = []string{}
	}
	httpx.JSON(w, 201, map[string]any{"slug": slug, "funciones": codigos})
}

// GET /api/peliculas/{slug}/poster
func (a *API) poster(w http.ResponseWriter, r *http.Request) {
	datos, tipo, err := a.s.Cartelera.Poster(r.Context(), r.PathValue("slug"))
	if err != nil {
		fallar(w, err)
		return
	}
	w.Header().Set("Content-Type", tipo)
	w.Header().Set("Content-Length", strconv.Itoa(len(datos)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(datos)
}

// POST /api/funciones  {pelicula, sala, inicio, formato, idioma, precio} → {codigo}
func (a *API) programarFuncion(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Pelicula string `json:"pelicula"`
		domain.NuevaFuncion
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	codigo, err := a.s.Cartelera.Programar(r.Context(), s.Pelicula, s.NuevaFuncion)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]string{"codigo": codigo})
}
