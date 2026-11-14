"use client";

// Estado de la aplicación.
//
// Todo se lee y escribe en la API en Go (NEXT_PUBLIC_API_URL): catálogo, ocupación de butacas, reservas (Redis, TTL
// 5 min), pagos (PostgreSQL, SELECT … FOR UPDATE), saldo, boletos, trivia, cola virtual, validación de acceso y,
// desde la autenticación en el servidor, las cuentas, la sesión y la bitácora. Si la API no responde se muestra un
// aviso para reintentar: nunca datos inventados.
//
// Sesión: el servidor entrega un token aleatorio al iniciar sesión. Se guarda en este navegador (localStorage) para
// sobrevivir a recargas y viaja en la cabecera Authorization. Al cargar la página se valida con el servidor; cerrar
// sesión lo revoca allá (no basta con olvidarlo aquí) y se cierra a la vez en todas las pestañas abiertas.

import { Suspense, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { LazyMotion, MotionConfig, domAnimation } from "framer-motion";
import { Lock, Timer } from "lucide-react";
import { Toasts, VentanaAviso, type Aviso, type Toast } from "@/components/layout/Notificaciones";
import { ServicioNoDisponible } from "@/components/layout/ServicioNoDisponible";
import { CambiarClave } from "@/components/auth/CambiarClave";
import { restablecerScroll } from "@/hooks/useBloqueoScroll";
import { limpiarTurnos } from "@/utils/turnoCola";
import { ApiError, alRechazarSesion, api, apiConfigurada, apiLocal, fijarToken } from "@/services/api";
import { olvidarPase, paseVigente } from "@/services/recaptcha";
import { boletoDeApi, funcionDeApi, funcionDeBoleto, movimientoDeApi, peliculaDeApi, usuarioDeApi } from "@/services/mapeadores";
import type { AsientosApi, CuentaApi, EventoAsientosApi, PosicionColaApi, SesionIniciadaApi } from "@/types/api";
import { PELICULAS, registrarPeliculas } from "@/data/catalogo";
import { PARAMETROS } from "@/data/parametros";
import type { Funcion, NuevaFuncion, NuevaPelicula, Pelicula } from "@/types/catalogo";
import type { Boleto, ModoApi, Movimiento, Reserva, Resultado, Usuario, Validacion } from "@/types/cuenta";
import { INICIAL, mensaje, type Estado } from "./estado";

/** `fuera`: butacas dañadas o fuera de servicio. `libres(aforo)` no cuenta dos veces una butaca vendida y luego retirada. */
type Ocupacion = { vendidos: Set<string>; retenidos: Set<string>; fuera: Set<string>; libres: (aforo: number) => number };
export type ResultadoCola = { ok: boolean; pos?: PosicionColaApi; error?: string; fuera?: boolean };

type Ctx = {
  listo: boolean;
  modoApi: ModoApi;
  usuario: Usuario | null;
  saldo: number;
  // false mientras la cuenta del cliente se carga desde la API: la interfaz no debe mostrar "0 créditos" provisional.
  saldoListo: boolean;
  movimientos: Movimiento[];
  boletos: Boleto[];
  reserva: Reserva | null;
  funciones: Funcion[];
  ocupacion: (funcionId: string) => Ocupacion;
  // Trae del backend las butacas vendidas y retenidas de esas funciones.
  cargarOcupacion: (funcionIds: string[]) => Promise<void>;
  // Tiempo real (SSE): reemplaza la foto de ocupación o aplica un cambio puntual de butacas.
  fijarOcupacion: (funcionId: string, datos: AsientosApi) => void;
  aplicarEventoAsientos: (ev: EventoAsientosApi) => void;
  // Candado individual de una butaca mientras el cliente arma su selección (SET NX EX en Redis).
  bloquearAsiento: (funcionId: string, asiento: string) => Promise<Resultado>;
  liberarAsientos: (funcionId: string, asientos: string[]) => void;
  // Butacas que el cliente ya tenía bloqueadas en esa función (p. ej. tras recargar la página).
  misBloqueos: (funcionId: string) => Promise<string[]>;
  refrescarCuenta: () => Promise<void>;
  // Sesión (verificada en el servidor con bcrypt).
  iniciarSesion: (correo: string, clave: string) => Promise<Resultado & { usuario?: Usuario }>;
  registrarCliente: (nombre: string, correo: string, clave: string) => Promise<Resultado & { usuario?: Usuario }>;
  cambiarClave: (actual: string, nueva: string) => Promise<Resultado>;
  cerrarSesionesTodas: () => Promise<Resultado>;
  logout: () => void;
  abrirCambioClave: () => void;
  // Gestión de usuarios: el administrador reconfirma su contraseña (el servidor lo exige durante 5 min).
  adminVerificado: boolean;
  verificarAdmin: (clave: string) => Promise<Resultado>;
  reservar: (funcionId: string, asientos: string[]) => Promise<Resultado>;
  cancelarReserva: (motivo?: "cancelada" | "expirada") => void;
  // Cobra la reserva en un solo paso: valida el saldo, debita y emite los boletos.
  pagar: () => Promise<Resultado & { boletos?: Boleto[]; total?: number; folio?: string }>;
  // Cola virtual de preestrenos en Redis. `fuera`: el usuario ya no está en la fila.
  colaEntrar: (eventoId: string) => Promise<ResultadoCola>;
  colaConsultar: (eventoId: string) => Promise<ResultadoCola>;
  colaSalir: (eventoId: string) => void;
  acreditarTrivia: (partidaId: string, aciertos: number) => Promise<{ ok: boolean; monto: number; error?: string }>;
  validarBoleto: (token: string) => Promise<Validacion>;
  // Catálogo editable (agente operativo y administrador), guardado en PostgreSQL.
  peliculas: Pelicula[];
  crearPelicula: (p: NuevaPelicula, funciones: NuevaFuncion[]) => Promise<Resultado & { id?: string; funciones?: number }>;
  programarFuncion: (f: NuevaFuncion & { peliculaId: string }) => Promise<Resultado & { id?: string }>;
  toast: (texto: string, tipo?: Toast["tipo"]) => void;
  // Inicio de sesión en ventana emergente: el usuario no sale de la página en la que está.
  loginAbierto: boolean;
  loginUsado: boolean;
  abrirLogin: (modo?: "login" | "registro") => void;
  cerrarLogin: () => void;
  modoLogin: "login" | "registro";
  // Trivia en ventana emergente: se abre encima de la compra sin desmontarla ni perder la selección.
  triviaAbierta: boolean;
  triviaUsada: boolean;
  abrirTrivia: () => void;
  cerrarTrivia: () => void;
};

const AppCtx = createContext<Ctx | null>(null);

// Un boleto se identifica por su token: si llega dos veces (p. ej. un pago repetido), se muestra una sola.
const sinDuplicados = (boletos: Boleto[]) => [...new Map(boletos.map((b) => [b.token, b])).values()];
const CLAVE = "cineight-v4"; // reserva en curso
const CLAVE_SESION = "cineight-sesion"; // token de la sesión del servidor
const CLAVES_ANTERIORES = ["cineight-prototipo-v3"]; // versiones que guardaban cuentas y contraseñas en el navegador

const leer = (k: string) => {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
};
const escribir = (k: string, v: string | null) => {
  try {
    if (v === null) localStorage.removeItem(k);
    else localStorage.setItem(k, v);
  } catch {}
};

// Red de seguridad del bloqueo de scroll: al cambiar de página, si no hay ventanas abiertas, el body se libera.
// Va en su propio componente (dentro de <Suspense>) para que usePathname no bloquee el prerender.
function ScrollPorRuta() {
  const ruta = usePathname();
  useEffect(() => restablecerScroll(), [ruta]);
  return null;
}

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [e, setE] = useState<Estado>(INICIAL);
  const [listo, setListo] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [aviso, setAviso] = useState<Aviso | null>(null);
  const [elevadaHasta, setElevadaHasta] = useState(0);
  const [loginAbierto, setLoginAbierto] = useState(false);
  const [loginUsado, setLoginUsado] = useState(false);
  const [modoLogin, setModoLogin] = useState<"login" | "registro">("login");
  const [triviaAbierta, setTriviaAbierta] = useState(false);
  const [triviaUsada, setTriviaUsada] = useState(false);
  const [cambioClave, setCambioClave] = useState(false);
  const [reloj, setReloj] = useState(0);

  // Estado que viene de la API (no se guarda en el navegador).
  const [modoApi, setModoApi] = useState<ModoApi>(apiConfigurada ? "verificando" : "sin-conexion");
  const [intento, setIntento] = useState(0);
  const [funcionesApi, setFuncionesApi] = useState<Funcion[]>([]);
  const [peliculasApi, setPeliculasApi] = useState<Pelicula[]>([]);
  const [asientosApi, setAsientosApi] = useState<Record<string, AsientosApi>>({});
  const [usuario, setUsuario] = useState<Usuario | null>(null);
  const [sesionRevisada, setSesionRevisada] = useState(false);
  const [cuentaApi, setCuentaApi] = useState<CuentaApi | null>(null);

  useEffect(() => {
    const t = setInterval(() => setReloj(Date.now()), 15000);
    return () => clearInterval(t);
  }, []);
  const ref = useRef({ e, usuario });
  useLayoutEffect(() => {
    ref.current = { e, usuario };
  }, [e, usuario]);

  useEffect(() => {
    // Las versiones anteriores guardaban cuentas con contraseñas (hash débil) en el navegador: se borran.
    CLAVES_ANTERIORES.forEach((k) => escribir(k, null));
    try {
      const raw = leer(CLAVE);
      // Hidratación desde localStorage: solo existe en el navegador, por eso se lee tras montar.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (raw) setE({ ...INICIAL, ...JSON.parse(raw) });
    } catch {}
    fijarToken(leer(CLAVE_SESION));
    setListo(true);
  }, []);

  useEffect(() => {
    if (listo) escribir(CLAVE, JSON.stringify(e));
  }, [e, listo]);

  const toast = useCallback((texto: string, tipo: Toast["tipo"] = "info") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, texto, tipo }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500);
  }, []);

  // Conexión con la API. En un hosting gratuito la API "duerme" y tarda hasta ~1 min en despertar: se reintenta hasta
  // entonces. Con la API en esta misma computadora basta un intento corto. `intento` permite reintentar a mano.
  useEffect(() => {
    if (!apiConfigurada) return;
    let vivo = true;
    const limite = Date.now() + (apiLocal ? 0 : 60000);
    const despertar = async (): Promise<void> => {
      try {
        await api.salud(apiLocal ? 2500 : 10000);
      } catch (err) {
        if (!vivo || Date.now() >= limite) throw err;
        await new Promise((r) => setTimeout(r, 2000));
        return despertar();
      }
    };
    despertar()
      // Películas y funciones juntas: una función nunca llega antes que su película.
      .then(() => Promise.all([api.funciones(), api.peliculas().catch(() => [])]))
      .then(([fs, ps]) => {
        if (!vivo) return;
        setPeliculasApi(ps.map(peliculaDeApi));
        setFuncionesApi(fs.map(funcionDeApi));
        setModoApi("conectado");
      })
      .catch(() => vivo && setModoApi("sin-conexion"));
    return () => {
      vivo = false;
    };
  }, [intento]);

  const reintentarConexion = () => {
    setModoApi("verificando");
    setIntento((n) => n + 1);
  };

  const cancelarReserva = useCallback((motivo: "cancelada" | "expirada" = "cancelada") => {
    const r = ref.current.e.reserva;
    if (!r) return;
    // Al cancelar se liberan las butacas en Redis; al expirar, el TTL ya lo hizo solo.
    if (motivo === "cancelada") api.cancelarReserva(r.id).catch(() => {});
    setE((s) => ({ ...s, reserva: null }));
    if (motivo === "expirada")
      setAviso({
        icono: Timer,
        titulo: "Tiempo agotado",
        texto: `Tu reserva de ${r.asientos.join(", ")} venció a los ${PARAMETROS.retencionMinutos} minutos y los asientos fueron liberados. No se cobró nada.`,
      });
  }, []);

  // Deja este navegador sin sesión (no llama al servidor: para eso está logout).
  const cerrarLocal = useCallback(
    (avisoCierre?: Aviso) => {
      if (ref.current.e.reserva) cancelarReserva();
      fijarToken(null);
      escribir(CLAVE_SESION, null);
      setUsuario(null);
      setCuentaApi(null);
      setElevadaHasta(0);
      setTriviaAbierta(false);
      setCambioClave(false);
      setLoginAbierto(false);
      limpiarTurnos();
      if (avisoCierre) setAviso(avisoCierre);
    },
    [cancelarReserva],
  );

  // Abre la sesión que entregó el servidor (inicio de sesión o registro).
  const abrirSesion = useCallback((s: SesionIniciadaApi) => {
    fijarToken(s.token);
    escribir(CLAVE_SESION, s.token);
    const u = usuarioDeApi(s.usuario);
    setUsuario(u);
    setElevadaHasta(0);
    return u;
  }, []);

  // Al conectar con la API se valida el token guardado: si expiró o se revocó, se descarta.
  useEffect(() => {
    if (modoApi !== "conectado") return;
    if (!leer(CLAVE_SESION)) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSesionRevisada(true);
      return;
    }
    let vivo = true;
    api
      .miSesion()
      .then((s) => {
        if (!vivo) return;
        setUsuario(usuarioDeApi(s.usuario));
        setElevadaHasta(s.elevada_hasta ? Date.parse(s.elevada_hasta) : 0);
      })
      .catch((err) => vivo && err instanceof ApiError && err.status === 401 && cerrarLocal())
      .finally(() => vivo && setSesionRevisada(true));
    return () => {
      vivo = false;
    };
  }, [modoApi, cerrarLocal]);

  // Si el servidor rechaza la sesión en cualquier solicitud (expiró, se cerró en otro dispositivo, cuenta dada de
  // baja), se cierra aquí también y se explica por qué.
  useEffect(() => {
    alRechazarSesion((motivo) => {
      if (!ref.current.usuario) return;
      cerrarLocal({ icono: Lock, titulo: "Sesión cerrada por seguridad", texto: motivo });
    });
    return () => alRechazarSesion(() => {});
  }, [cerrarLocal]);

  // Varias pestañas: cerrar sesión en una la cierra en todas; iniciarla en otra la toma aquí también.
  useEffect(() => {
    const cambio = (ev: StorageEvent) => {
      if (ev.key !== CLAVE_SESION) return;
      if (!ev.newValue) {
        if (ref.current.usuario) cerrarLocal();
        return;
      }
      fijarToken(ev.newValue);
      api
        .miSesion()
        .then((s) => setUsuario(usuarioDeApi(s.usuario)))
        .catch(() => {});
    };
    window.addEventListener("storage", cambio);
    return () => window.removeEventListener("storage", cambio);
  }, [cerrarLocal]);

  // Cuenta de créditos del cliente.
  const esCliente = usuario?.rol === "cliente" && !usuario.debeCambiarClave;
  const idCliente = esCliente ? usuario.id : null;
  useEffect(() => {
    if (modoApi !== "conectado" || idCliente === null) return;
    let vivo = true;
    api
      .cuenta()
      .then((c) => vivo && setCuentaApi(c))
      .catch((err) => vivo && !(err instanceof ApiError && err.status === 401) && toast(`No se pudo cargar tu cuenta: ${mensaje(err)}`, "error"));
    return () => {
      vivo = false;
    };
  }, [modoApi, idCliente, toast]);

  const refrescarCuenta = useCallback(async () => {
    if (ref.current.usuario?.rol !== "cliente") return;
    try {
      setCuentaApi(await api.cuenta());
    } catch {}
  }, []);

  // Depende de modoApi: al conectarse cambia su identidad y las páginas que la usan vuelven a pedir los datos.
  const cargarOcupacion = useCallback(
    async (ids: string[]) => {
      if (modoApi !== "conectado" || ids.length === 0) return;
      let nuevos: Record<string, AsientosApi> = {};
      if (ids.length > 8) {
        // Muchas funciones (panel): una sola solicitud en vez de una por función.
        const todas = await api.ocupacion().catch(() => ({}) as Record<string, AsientosApi>);
        nuevos = Object.fromEntries(ids.filter((id) => todas[id]).map((id) => [id, todas[id]]));
      } else {
        const pares = await Promise.all(ids.map((id) => api.asientos(id).then((r) => [id, r] as const).catch(() => null)));
        nuevos = Object.fromEntries(pares.filter((p) => p !== null));
      }
      if (Object.keys(nuevos).length) setAsientosApi((a) => ({ ...a, ...nuevos }));
    },
    [modoApi],
  );

  const fijarOcupacion = useCallback((funcionId: string, datos: AsientosApi) => setAsientosApi((a) => ({ ...a, [funcionId]: datos })), []);

  // Aplica un cambio recibido por SSE sin volver a pedir toda la ocupación.
  const aplicarEventoAsientos = useCallback((ev: EventoAsientosApi) => {
    setAsientosApi((a) => {
      const previo = a[ev.funcion] ?? { vendidos: [], retenidos: [] };
      const retenidos = new Set(previo.retenidos);
      const vendidos = new Set(previo.vendidos);
      const fuera = new Set(previo.fuera_servicio);
      for (const as of ev.asientos) {
        if (ev.tipo === "bloqueado") retenidos.add(as);
        else retenidos.delete(as);
        if (ev.tipo === "vendido") vendidos.add(as);
        if (ev.tipo === "retirado") fuera.add(as);
        if (ev.tipo === "liberado") fuera.delete(as);
      }
      return { ...a, [ev.funcion]: { vendidos: [...vendidos], retenidos: [...retenidos], fuera_servicio: [...fuera] } };
    });
  }, []);

  const bloquearAsiento = useCallback(async (funcionId: string, asiento: string): Promise<Resultado> => {
    if (!ref.current.usuario) return { ok: false, error: "Debes iniciar sesión para elegir butacas." };
    try {
      await api.bloquearAsiento(funcionId, asiento);
      return { ok: true };
    } catch (err) {
      return { ok: false, error: mensaje(err) };
    }
  }, []);

  const liberarAsientos = useCallback((funcionId: string, asientos: string[]) => {
    if (!ref.current.usuario || asientos.length === 0) return;
    api.desbloquearAsientos(funcionId, asientos).catch(() => {});
  }, []);

  const misBloqueos = useCallback(async (funcionId: string) => {
    if (ref.current.usuario?.rol !== "cliente") return [];
    try {
      return (await api.misBloqueos(funcionId)).propios ?? [];
    } catch {
      return [];
    }
  }, []);

  // Películas creadas desde el panel (PostgreSQL). Se registran en el catálogo (pelicula(id)) durante el render,
  // antes de que los hijos las consulten.
  const peliculas = useMemo(() => {
    registrarPeliculas(peliculasApi);
    return [...PELICULAS, ...peliculasApi];
  }, [peliculasApi]);

  const recargarCatalogo = useCallback(async () => {
    const [fs, ps] = await Promise.all([api.funciones(), api.peliculas()]);
    setPeliculasApi(ps.map(peliculaDeApi));
    setFuncionesApi(fs.map(funcionDeApi));
  }, []);

  // La cartelera es la de PostgreSQL. Se agregan las funciones de los boletos del cliente (con sus datos reales)
  // para mostrar también los de funciones ya terminadas, que la API ya no lista.
  const boletosApi = cuentaApi?.boletos;
  const funciones = useMemo(() => {
    const porCodigo = new Map(funcionesApi.map((f) => [f.id, f]));
    for (const b of boletosApi ?? []) if (!porCodigo.has(b.funcion)) porCodigo.set(b.funcion, funcionDeBoleto(b));
    return [...porCodigo.values()];
  }, [funcionesApi, boletosApi]);

  const ocupacion = useCallback(
    (funcionId: string): Ocupacion => {
      const d = asientosApi[funcionId];
      const vendidos = new Set(d?.vendidos ?? []);
      const retenidos = new Set(d?.retenidos ?? []);
      const fuera = new Set(d?.fuera_servicio ?? []);
      const ocupadas = new Set([...vendidos, ...retenidos, ...fuera]).size;
      return { vendidos, retenidos, fuera, libres: (aforo: number) => Math.max(0, aforo - ocupadas) };
    },
    [asientosApi],
  );

  // Temporizador de expiración (Redis libera las butacas con el TTL; aquí sólo se avisa al cliente)
  useEffect(() => {
    if (!e.reserva) return;
    const ms = e.reserva.expira - Date.now();
    const t = setTimeout(() => cancelarReserva("expirada"), Math.max(0, ms));
    return () => clearTimeout(t);
  }, [e.reserva, cancelarReserva]);

  // Cierra la sesión en el servidor (revoca el token) y en este navegador.
  const logout = useCallback(() => {
    if (ref.current.usuario) api.logout().catch(() => {});
    cerrarLocal();
  }, [cerrarLocal]);

  // Cierre de sesión automático por inactividad (RNF-24). El servidor también la vence a los mismos minutos.
  const ultimaActividad = useRef(0);
  useEffect(() => {
    ultimaActividad.current = Date.now();
    const marcar = () => (ultimaActividad.current = Date.now());
    const eventos = ["mousemove", "keydown", "pointerdown", "scroll", "touchstart"];
    eventos.forEach((ev) => window.addEventListener(ev, marcar, { passive: true }));
    const t = setInterval(() => {
      if (ref.current.usuario && Date.now() - ultimaActividad.current > PARAMETROS.inactividadMinutos * 60000) {
        if (ref.current.usuario) api.logout().catch(() => {});
        cerrarLocal({
          icono: Lock,
          titulo: "Sesión cerrada por seguridad",
          texto: `No detectamos actividad durante ${PARAMETROS.inactividadMinutos} minutos. Vuelve a iniciar sesión para continuar.`,
        });
      }
    }, 10000);
    return () => {
      eventos.forEach((ev) => window.removeEventListener(ev, marcar));
      clearInterval(t);
    };
  }, [cerrarLocal]);

  const reservaPropia = e.reserva && e.reserva.titular === usuario?.correo ? e.reserva : null;
  const cuentaCliente = esCliente ? cuentaApi : null;
  // Listo cuando se leyó el navegador y, con la API conectada, el servidor confirmó (o descartó) la sesión guardada.
  const todoListo = listo && (modoApi !== "conectado" || sesionRevisada);

  const valor: Ctx = {
    listo: todoListo,
    modoApi,
    usuario,
    // Mientras la cuenta del cliente se carga no se muestra un "0 créditos" provisional (heurística 1 de Nielsen).
    saldoListo: usuario?.rol !== "cliente" || cuentaCliente !== null,
    saldo: cuentaCliente?.saldo ?? 0,
    movimientos: cuentaCliente?.movimientos.map(movimientoDeApi) ?? [],
    boletos: sinDuplicados(cuentaCliente?.boletos.map((b) => boletoDeApi(b, usuario!.correo)) ?? []),
    reserva: reservaPropia,
    funciones,
    ocupacion,
    cargarOcupacion,
    fijarOcupacion,
    aplicarEventoAsientos,
    bloquearAsiento,
    liberarAsientos,
    misBloqueos,
    refrescarCuenta,
    toast,
    loginAbierto,
    loginUsado,
    modoLogin,
    abrirLogin: (modo = "login") => {
      setModoLogin(modo);
      setLoginUsado(true);
      setLoginAbierto(true);
    },
    cerrarLogin: () => setLoginAbierto(false),
    triviaAbierta,
    triviaUsada,
    abrirTrivia: () => {
      setTriviaUsada(true);
      setTriviaAbierta(true);
    },
    cerrarTrivia: () => setTriviaAbierta(false),
    iniciarSesion: async (correo, clave) => {
      try {
        const u = abrirSesion(await api.login(correo, clave));
        toast(`Bienvenido, ${u.nombre}`, "ok");
        return { ok: true, usuario: u };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    registrarCliente: async (nombre, correo, clave) => {
      try {
        const s = await api.registro(nombre, correo, clave, paseVigente());
        olvidarPase();
        return { ok: true, usuario: abrirSesion(s) };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    cambiarClave: async (actual, nueva) => {
      try {
        await api.cambiarClave(actual, nueva);
        setUsuario((u) => (u ? { ...u, debeCambiarClave: false } : u));
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    cerrarSesionesTodas: async () => {
      try {
        await api.logoutTodas();
        cerrarLocal();
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    logout,
    abrirCambioClave: () => setCambioClave(true),
    adminVerificado: elevadaHasta > reloj,
    verificarAdmin: async (clave) => {
      try {
        const r = await api.reconfirmar(clave);
        setReloj(Date.now());
        setElevadaHasta(Date.parse(r.elevada_hasta));
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    reservar: async (funcionId, asientos) => {
      const u = ref.current.usuario;
      const actual = ref.current.e.reserva;
      if (!u) return { ok: false, error: "Debes iniciar sesión para reservar asientos." };
      if (actual && actual.titular === u.correo) return { ok: false, error: "Ya tienes una reserva activa." };
      if (asientos.length === 0) return { ok: false, error: "Selecciona al menos una butaca." };
      if (asientos.length > PARAMETROS.maxButacasPorTransaccion) return { ok: false, error: `Máximo ${PARAMETROS.maxButacasPorTransaccion} butacas por transacción.` };
      // POST /api/reservas: SET NX + TTL en Redis. El servidor decide si las butacas siguen libres
      // y aplica el límite de boletos del preestreno.
      try {
        const res = await api.crearReserva(funcionId, asientos);
        // El vencimiento se cuenta con el reloj local para no depender del desfase entre equipos.
        const r: Reserva = { id: res.reserva_id, titular: u.correo, funcionId, asientos, total: res.total, expira: Date.now() + PARAMETROS.retencionMinutos * 60000 - 1000 };
        setE((st) => ({ ...st, reserva: r }));
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      } finally {
        cargarOcupacion([funcionId]);
      }
    },
    cancelarReserva,
    pagar: async () => {
      const u = ref.current.usuario;
      const r = ref.current.e.reserva;
      if (!r || !u || r.titular !== u.correo) return { ok: false, error: "La reserva expiró." };
      const f = funciones.find((x) => x.id === r.funcionId)!;
      const total = r.total ?? f.precio * r.asientos.length;
      // POST /api/reservas/{id}/pagar: transacción con SELECT … FOR UPDATE; repetirla no vuelve a cobrar.
      try {
        const p = await api.pagar(r.id);
        const nuevos = p.boletos.map((b) => boletoDeApi(b, r.titular));
        setE((st) => ({ ...st, reserva: null }));
        await refrescarCuenta();
        cargarOcupacion([r.funcionId]);
        return { ok: true, boletos: nuevos, total: p.total ?? total, folio: p.reserva_id };
      } catch (err) {
        const status = err instanceof ApiError ? err.status : 0;
        if (status === 410 || status === 409 || status === 404) setE((st) => ({ ...st, reserva: null }));
        if (status === 402) {
          await refrescarCuenta();
          return { ok: false, error: `Saldo insuficiente: necesitas ${total} créditos y tienes ${cuentaApi?.saldo ?? 0}.` };
        }
        return { ok: false, error: mensaje(err) };
      }
    },
    colaEntrar: async (eventoId) => {
      if (!ref.current.usuario) return { ok: false, error: "Debes iniciar sesión." };
      try {
        const pos = await api.entrarCola(eventoId, paseVigente());
        olvidarPase();
        return { ok: true, pos };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    colaConsultar: async (eventoId) => {
      if (!ref.current.usuario) return { ok: false, error: "Debes iniciar sesión." };
      try {
        return { ok: true, pos: await api.posicionCola(eventoId) };
      } catch (err) {
        return { ok: false, error: mensaje(err), fuera: err instanceof ApiError && err.status === 404 };
      }
    },
    colaSalir: (eventoId) => {
      if (ref.current.usuario) api.salirCola(eventoId).catch(() => {});
    },
    acreditarTrivia: async (partidaId, aciertos) => {
      if (!ref.current.usuario) return { ok: false, monto: 0, error: "Debes iniciar sesión." };
      // El servidor calcula el premio a partir de los aciertos y no acredita dos veces la misma partida (RF-64).
      try {
        const r = await api.acreditarTrivia(partidaId, aciertos);
        await refrescarCuenta();
        return { ok: r.acreditado, monto: r.monto };
      } catch (err) {
        return { ok: false, monto: 0, error: mensaje(err) };
      }
    },
    validarBoleto: async (entrada) => {
      // Acepta el contenido del QR (JSON con el código dinámico y su ventana) o "TOKEN CÓDIGO" escrito a mano
      // (ventana 0: el servidor prueba el minuto anterior, el actual y el siguiente). El servidor comprueba el
      // código con su secreto: un QR vencido, falso o sin código no consume el boleto.
      let tok = "";
      let codigo = "";
      let ventana = 0;
      const texto = entrada.trim();
      if (texto.startsWith("{")) {
        try {
          const q = JSON.parse(texto);
          tok = String(q.t ?? "");
          codigo = String(q.c ?? "");
          ventana = Number(q.w) || 0;
        } catch {}
      } else {
        [tok = "", codigo = ""] = texto.split(/\s+/);
      }
      if (!tok) return { ok: false, motivo: "Boleto inexistente" };
      if (!codigo) return { ok: false, motivo: "Falta el código de 6 caracteres que aparece bajo el QR." };
      try {
        const r = await api.validarBoleto(tok, codigo, ventana);
        return { ok: r.ok, motivo: r.motivo, boleto: r.boleto && boletoDeApi(r.boleto, r.titular ?? "") };
      } catch (err) {
        return { ok: false, motivo: mensaje(err) };
      }
    },
    peliculas,
    crearPelicula: async (p, nuevas) => {
      const u = ref.current.usuario;
      if (u?.rol !== "agente" && u?.rol !== "admin") return { ok: false, error: "Sólo el agente operativo o el administrador pueden crear películas." };
      // POST /api/peliculas: ficha + póster + funciones en una sola transacción (si una función choca, no se guarda nada).
      try {
        const r = await api.crearPelicula({
          titulo: p.titulo,
          titulo_original: p.tituloOriginal,
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
          trailer: p.trailer ?? null,
          estreno_en_dias: p.estrenoEnDias ?? null,
          poster: p.poster,
          funciones: nuevas.map((f) => ({ sala: f.salaId, inicio: f.inicio, formato: f.formato, idioma: f.idioma, precio: f.precio })),
        });
        await recargarCatalogo();
        return { ok: true, id: r.slug, funciones: r.funciones.length };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    programarFuncion: async ({ peliculaId, ...f }) => {
      try {
        const r = await api.programarFuncion(peliculaId, { sala: f.salaId, inicio: f.inicio, formato: f.formato, idioma: f.idioma, precio: f.precio });
        await recargarCatalogo();
        return { ok: true, id: r.codigo };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
  };

  return (
    <AppCtx.Provider value={valor}>
      {/* LazyMotion carga sólo las funciones de animación que se usan (domAnimation) y `strict`
          impide volver a importar el `motion` completo por accidente. Respeta "reducir movimiento" del sistema. */}
      <LazyMotion features={domAnimation} strict>
        <Suspense fallback={null}>
          <ScrollPorRuta />
        </Suspense>
        <MotionConfig reducedMotion="user">
          {/* Se espera la respuesta de la API: así nunca se pinta una cartelera vacía o que luego cambia. */}
          {!todoListo || modoApi === "verificando" ? (
            <div className="py-24 text-center text-sm text-tenue">
              {listo && !apiLocal ? "Conectando con el servidor… la primera visita puede tardar hasta un minuto." : "Cargando…"}
            </div>
          ) : modoApi === "sin-conexion" ? (
            <ServicioNoDisponible configurada={apiConfigurada} onReintentar={reintentarConexion} />
          ) : (
            children
          )}
          {/* Contraseña temporal: hay que cambiarla antes de usar la cuenta (también se abre desde el menú). */}
          {usuario && (usuario.debeCambiarClave || cambioClave) && <CambiarClave obligatoria={usuario.debeCambiarClave} onCerrar={() => setCambioClave(false)} />}
          <Toasts toasts={toasts} />
          <VentanaAviso aviso={aviso} onCerrar={() => setAviso(null)} />
        </MotionConfig>
      </LazyMotion>
    </AppCtx.Provider>
  );
}

export function useApp() {
  const c = useContext(AppCtx);
  if (!c) throw new Error("useApp fuera de AppProvider");
  return c;
}
