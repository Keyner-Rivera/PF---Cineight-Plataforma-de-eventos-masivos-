// Cliente HTTP de la API en Go (backend/cmd/api). La URL base sale de NEXT_PUBLIC_API_URL.
// Si la variable no está definida o la API no responde, la interfaz muestra "Servicio no disponible" (ver `modoApi`).
//
// Sesión: el servidor entrega un token al iniciar sesión; aquí viaja en la cabecera Authorization (nunca en la URL,
// que queda en historiales y registros). Como el navegador no la envía solo, otra página no puede hacer solicitudes
// en nombre del usuario (CSRF). Si el servidor responde 401 a una solicitud con sesión, se avisa a la app para
// cerrar la sesión local (expiró, se cerró en otro dispositivo o la cuenta se dio de baja).

import type {
  AsientosApi,
  BitacoraApi,
  ButacaFueraServicioApi,
  CaptchaApi,
  CodigosQrApi,
  ComprobanteApi,
  CuentaApi,
  FuncionApi,
  HistorialClienteApi,
  NuevaFuncionApi,
  PeliculaApi,
  MetricasApi,
  PagoApi,
  PosicionColaApi,
  ReservaApi,
  SesionApi,
  SesionIniciadaApi,
  TriviaApi,
  MotivoFueraServicio,
  UsuarioApi,
  ValidacionApi,
  VerificacionComprobanteApi,
} from "@/types/api";
import type { Rol } from "@/types/catalogo";

const BASE = (process.env.NEXT_PUBLIC_API_URL ?? "").replace(/\/+$/, "");

export const apiConfigurada = BASE !== "";
/** La API corre en esta misma computadora (desarrollo): responde al instante o no está levantada. */
export const apiLocal = /^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/.test(BASE);

/** Error de la API con su código HTTP (0 = sin conexión o tiempo agotado). */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public datos?: unknown,
  ) {
    super(message);
  }
}

let token: string | null = null;
let alPerderSesion: ((motivo: string) => void) | null = null;

/** Token de la sesión actual (lo fija la app al iniciar sesión o al recuperarla del almacenamiento). */
export function fijarToken(t: string | null) {
  token = t;
}

/** La app registra aquí qué hacer cuando el servidor rechaza la sesión (401). */
export function alRechazarSesion(fn: (motivo: string) => void) {
  alPerderSesion = fn;
}

async function pedir<T>(ruta: string, init: RequestInit = {}, timeoutMs = 8000): Promise<T> {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  const conSesion = token !== null;
  try {
    const headers: Record<string, string> = { ...(init.headers as Record<string, string>) };
    if (init.body) headers["Content-Type"] = "application/json";
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await fetch(BASE + ruta, { ...init, headers, signal: ctrl.signal, cache: "no-store", credentials: "omit" });
    const datos = await res.json().catch(() => ({}));
    if (!res.ok) {
      const mensaje = (datos as { error?: string }).error ?? `Error ${res.status}`;
      if (res.status === 401 && conSesion) alPerderSesion?.(mensaje);
      throw new ApiError(res.status, mensaje, datos);
    }
    return datos as T;
  } catch (e) {
    if (e instanceof ApiError) throw e;
    throw new ApiError(0, ctrl.signal.aborted ? "La API no respondió a tiempo." : "No se pudo conectar con la API.");
  } finally {
    clearTimeout(t);
  }
}

const post = <T>(ruta: string, cuerpo?: unknown, timeoutMs?: number) =>
  pedir<T>(ruta, { method: "POST", body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) }, timeoutMs);
const del = <T>(ruta: string, cuerpo?: unknown) => pedir<T>(ruta, { method: "DELETE", body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
const seg = encodeURIComponent;

export const api = {
  salud: (timeoutMs = 2500) => pedir<{ estado: string }>("/api/salud", {}, timeoutMs),

  // Sesión (bcrypt en el servidor; puede tardar un poco más en un hosting gratuito)
  registro: (nombre: string, correo: string, clave: string, pase?: string) =>
    post<SesionIniciadaApi>("/api/auth/registro", { nombre, correo, clave, ...(pase ? { pase } : {}) }, 20000),
  login: (correo: string, clave: string) => post<SesionIniciadaApi>("/api/auth/login", { correo, clave }, 20000),
  miSesion: () => pedir<SesionApi>("/api/auth/sesion"),
  /** keepalive: el cierre llega al servidor aunque la pestaña se esté cerrando. */
  logout: () => pedir<{ estado: string }>("/api/auth/logout", { method: "POST", keepalive: true }),
  logoutTodas: () => post<{ sesiones_cerradas: number }>("/api/auth/logout-todas"),
  cambiarClave: (actual: string, nueva: string) => post<{ estado: string }>("/api/auth/clave", { actual, nueva }, 20000),
  reconfirmar: (clave: string) => post<{ elevada_hasta: string }>("/api/auth/reconfirmar", { clave }, 20000),

  // Catálogo y mapa de butacas
  funciones: () => pedir<FuncionApi[]>("/api/funciones"),
  asientos: (funcion: string) => pedir<AsientosApi>(`/api/funciones/${seg(funcion)}/asientos`),
  /** Butacas que el cliente tiene bloqueadas en la función (para restaurar su selección al recargar). */
  misBloqueos: (funcion: string) => pedir<{ propios: string[] }>(`/api/funciones/${seg(funcion)}/mis-bloqueos`),
  /** Ocupación de todas las funciones vigentes en una sola respuesta (panel, listados grandes). */
  ocupacion: () => pedir<Record<string, AsientosApi>>("/api/ocupacion"),
  /** Candado individual de una butaca (SET NX EX en Redis) mientras el cliente arma su selección. */
  bloquearAsiento: (funcion: string, asiento: string) => post<{ asiento: string; estado: string }>(`/api/funciones/${seg(funcion)}/asientos/${seg(asiento)}/bloqueo`),
  /** Suelta butacas bloqueadas por el cliente. keepalive: se completa aunque cierre la pestaña o cambie de página. */
  desbloquearAsientos: (funcion: string, asientos: string[]) =>
    pedir<{ estado: string }>(`/api/funciones/${seg(funcion)}/bloqueos`, { method: "DELETE", keepalive: true, body: JSON.stringify({ asientos }) }),
  /** URL del stream SSE con los cambios de butacas de una función (público). */
  urlStream: (funcion: string) => `${BASE}/api/funciones/${seg(funcion)}/stream`,

  // Catálogo editable (agente operativo y administrador)
  peliculas: () => pedir<PeliculaApi[]>("/api/peliculas"),
  /** Crea la película con su póster (data URL) y sus funciones en una sola transacción. */
  crearPelicula: (datos: Omit<PeliculaApi, "slug" | "poster_url"> & { poster: string; funciones: NuevaFuncionApi[] }) =>
    pedir<{ slug: string; funciones: string[] }>("/api/peliculas", { method: "POST", body: JSON.stringify(datos) }, 20000),
  programarFuncion: (pelicula: string, f: NuevaFuncionApi) => post<{ codigo: string }>("/api/funciones", { pelicula, ...f }),
  /** Butacas dañadas o fuera de servicio de una sala: no se venden en ninguna de sus funciones. */
  fueraServicio: (sala: string) => pedir<ButacaFueraServicioApi[]>(`/api/salas/${seg(sala)}/fuera-servicio`),
  retirarButaca: (sala: string, asiento: string, motivo: MotivoFueraServicio) => post<{ estado: string }>(`/api/salas/${seg(sala)}/fuera-servicio/${seg(asiento)}`, { motivo }),
  habilitarButaca: (sala: string, asiento: string) => del<{ estado: string }>(`/api/salas/${seg(sala)}/fuera-servicio/${seg(asiento)}`),
  /** URL absoluta de un recurso de la API (p. ej. el póster de una película creada desde el panel). */
  url: (ruta: string) => BASE + ruta,

  // Compra (el cliente se identifica con su sesión)
  crearReserva: (funcion: string, asientos: string[]) => post<ReservaApi>("/api/reservas", { funcion, asientos }),
  pagar: (reservaId: string) => post<PagoApi>(`/api/reservas/${seg(reservaId)}/pagar`),
  cancelarReserva: (reservaId: string) => del<{ estado: string }>(`/api/reservas/${seg(reservaId)}`),

  // Cuenta y créditos del cliente
  cuenta: () => pedir<CuentaApi>("/api/cuenta"),
  acreditarTrivia: (partida: string, aciertos: number) => post<TriviaApi>("/api/creditos/trivia", { partida, aciertos }),
  /** Códigos dinámicos del QR de un boleto propio para los próximos minutos (firmados por el servidor). */
  codigosBoleto: (token: string) => pedir<CodigosQrApi>(`/api/boletos/${seg(token)}/codigos`),
  /** Comprobante firmado de una compra propia. */
  comprobante: (folio: string) => pedir<ComprobanteApi>(`/api/compras/${seg(folio)}`),
  /** Verificación pública de un comprobante (lo que muestra su QR). */
  verificarComprobante: (folio: string, firma: string) => pedir<VerificacionComprobanteApi>(`/api/comprobantes/${seg(folio)}/verificar?firma=${seg(firma)}`),

  // Cola virtual de preestrenos (Redis). Quien espera consulta su posición cada pocos segundos o pierde su lugar.
  entrarCola: (evento: string, pase?: string) => post<PosicionColaApi>(`/api/cola/${seg(evento)}`, pase ? { pase } : {}),
  posicionCola: (evento: string) => pedir<PosicionColaApi>(`/api/cola/${seg(evento)}`),
  salirCola: (evento: string) => del<{ estado: string }>(`/api/cola/${seg(evento)}`),

  // Operación
  /** Marca el boleto como usado si `codigo` es el código dinámico vigente de su QR (ventana 0 = escrito a mano). */
  validarBoleto: (token: string, codigo: string, ventana: number) => post<ValidacionApi>(`/api/boletos/${seg(token)}/validar`, { codigo, ventana }),
  metricas: () => pedir<MetricasApi>("/api/metricas"),
  bitacora: () => pedir<BitacoraApi[]>("/api/bitacora"),
  /** Compras de cada cliente agrupadas por película (estadísticas del administrador). */
  historialClientes: () => pedir<HistorialClienteApi[]>("/api/clientes/historial"),

  // Usuarios y roles (administrador; crear, editar y dar de baja exigen reconfirmar la contraseña)
  usuarios: () => pedir<UsuarioApi[]>("/api/usuarios"),
  crearUsuario: (d: { nombre: string; correo: string; rol: Rol; clave: string }) => post<UsuarioApi>("/api/usuarios", d, 20000),
  editarUsuario: (id: number, d: { nombre: string; rol: Rol }) => post<{ estado: string }>(`/api/usuarios/${id}`, d),
  estadoUsuario: (id: number, activo: boolean) => post<{ estado: string }>(`/api/usuarios/${id}/estado`, { activo }),

  // Seguridad
  /** Verifica en el servidor un token de reCAPTCHA v3 antes de procesar credenciales. */
  verificarCaptcha: (token: string, accion: string) => post<CaptchaApi>("/api/captcha/verificar", { token, accion }),
};
