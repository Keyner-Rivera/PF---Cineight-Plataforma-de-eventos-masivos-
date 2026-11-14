// Conversión de las respuestas de la API en Go (snake_case) al modelo de la interfaz.

import { api } from "./api";
import type { BoletoApi, FuncionApi, MovimientoApi, PeliculaApi, UsuarioApi } from "@/types/api";
import type { Funcion, Pelicula } from "@/types/catalogo";
import type { Boleto, Movimiento, Usuario } from "@/types/cuenta";

export const funcionDeApi = (f: FuncionApi): Funcion => ({
  id: f.codigo,
  peliculaId: f.pelicula,
  salaId: f.sala,
  inicio: f.inicio,
  formato: f.formato,
  idioma: f.idioma,
  precio: f.precio,
  ...(f.evento ? { eventoId: f.evento } : {}),
});

const ESTADO_BOLETO: Record<BoletoApi["estado"], Boleto["estado"]> = { valido: "VÁLIDO", utilizado: "UTILIZADO", anulado: "ANULADO", reembolsado: "REEMBOLSADO" };
export const boletoDeApi = (b: BoletoApi, titular: string): Boleto => ({
  token: b.token,
  funcionId: b.funcion,
  asiento: b.asiento,
  precio: b.precio,
  estado: ESTADO_BOLETO[b.estado],
  emitido: b.fecha,
  titular,
  folio: b.reserva,
});

/** Función de un boleto, tal como la guarda PostgreSQL (puede estar terminada y ya no venir en GET /api/funciones). */
export const funcionDeBoleto = (b: BoletoApi): Funcion => ({
  id: b.funcion,
  peliculaId: b.pelicula,
  salaId: b.sala,
  inicio: b.inicio,
  formato: b.formato,
  idioma: b.idioma,
  precio: b.precio,
});

const TIPO_MOV: Record<MovimientoApi["tipo"], Movimiento["tipo"]> = { recarga: "recarga", retencion: "retención", debito: "débito", liberacion: "liberación", reembolso: "reembolso" };
function referenciaLegible(ref: string) {
  if (ref.startsWith("bono-bienvenida")) return "Bono de bienvenida";
  if (ref.startsWith("trivia:")) return `Trivia ${ref.slice(7)}`;
  if (ref.startsWith("R-")) return `Pago ${ref}`;
  return ref;
}
export const movimientoDeApi = (x: MovimientoApi): Movimiento => ({ id: String(x.id), tipo: TIPO_MOV[x.tipo], monto: x.monto, ref: referenciaLegible(x.referencia), fecha: x.fecha });

// Las películas creadas desde el panel guardan el póster en PostgreSQL; la API lo sirve en /api/peliculas/{slug}/poster.
export const peliculaDeApi = (p: PeliculaApi): Pelicula => ({
  id: p.slug,
  titulo: p.titulo,
  tituloOriginal: p.titulo_original,
  categoria: p.categoria,
  clasificacion: p.clasificacion,
  duracion: p.duracion,
  genero: p.genero,
  idioma: p.idioma,
  formatos: p.formatos,
  director: p.director,
  reparto: p.reparto,
  anio: p.anio,
  sinopsis: p.sinopsis,
  ...(p.trailer ? { trailer: p.trailer } : {}),
  ...(p.estreno_en_dias != null ? { estrenoEnDias: p.estreno_en_dias } : {}),
  poster: api.url(p.poster_url),
  acento: "#2a0b0b",
  personalizada: true,
});

/** Usuario de la sesión tal como lo usa la interfaz. */
export const usuarioDeApi = (u: UsuarioApi): Usuario => ({ id: u.id, nombre: u.nombre, correo: u.correo, rol: u.rol, debeCambiarClave: u.debe_cambiar_clave });
