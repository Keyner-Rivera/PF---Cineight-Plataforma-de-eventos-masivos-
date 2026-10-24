"use client";

// Estado de la aplicación.
//
// El catálogo, la ocupación de butacas, las reservas (Redis, TTL 5 min), los pagos (PostgreSQL, SELECT … FOR UPDATE),
// el saldo, los boletos, la trivia, la cola virtual y la validación de acceso se leen y escriben en la API en Go
// (NEXT_PUBLIC_API_URL). Si la API no responde se muestra un aviso para reintentar: nunca datos inventados.
// El inicio de sesión, los roles, la gestión de usuarios y la bitácora todavía viven en el navegador (pendiente: JWT + bcrypt).

import { Suspense, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { LazyMotion, MotionConfig, domAnimation } from "framer-motion";
import { Lock, Timer } from "lucide-react";
import { Toasts, VentanaAviso, type Aviso, type Toast } from "@/components/layout/Notificaciones";
import { ServicioNoDisponible } from "@/components/layout/ServicioNoDisponible";
import { codigoDinamico, ventanaActual } from "@/utils/seguridad";
import { restablecerScroll } from "@/hooks/useBloqueoScroll";
import { limpiarTurnos } from "@/utils/turnoCola";
import { ApiError, api, apiConfigurada, apiLocal } from "@/services/api";
import { olvidarPase, paseVigente } from "@/services/recaptcha";
import { boletoDeApi, funcionDeApi, funcionDeBoleto, movimientoDeApi, peliculaDeApi } from "@/services/mapeadores";
import type { AsientosApi, CuentaApi, EventoAsientosApi, PosicionColaApi, SesionApi } from "@/types/api";
import { PELICULAS, registrarPeliculas } from "@/data/catalogo";
import { normalizarCorreo, normalizarNombre, validarClaveNueva, validarCorreo, validarNombre } from "@/utils/validacion";
import { PARAMETROS } from "@/data/parametros";
import type { Funcion, NuevaFuncion, NuevaPelicula, Pelicula, Rol } from "@/types/catalogo";
import type { Boleto, Cuenta, Log, ModoApi, Movimiento, Reserva, Resultado, Usuario, Validacion } from "@/types/cuenta";
import { INICIAL, adminsActivos, conCuenta, conPersonal, cuentaNueva, hashClave, log, mensaje, type Estado } from "./estado";

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
  cuentas: Record<string, Cuenta>;
  reserva: Reserva | null;
  bitacora: Log[];
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
  iniciarSesion: (correo: string, clave: string) => Resultado;
  registrarCliente: (nombre: string, correo: string, clave: string) => Resultado;
  // Gestión de usuarios: sólo Administrador del Sistema y tras reconfirmar su contraseña.
  adminVerificado: boolean;
  verificarAdmin: (clave: string) => boolean;
  crearUsuario: (d: { nombre: string; correo: string; rol: Rol; clave: string }) => Resultado;
  editarUsuario: (correo: string, d: { nombre: string; rol: Rol }) => Resultado;
  cambiarEstadoUsuario: (correo: string, activo: boolean) => Resultado;
  logout: () => void;
  reservar: (funcionId: string, asientos: string[]) => Promise<Resultado>;
  cancelarReserva: (motivo?: "cancelada" | "expirada") => void;
  // Cobra la reserva en un solo paso: valida el saldo, debita y emite los boletos.
  pagar: () => Promise<Resultado & { boletos?: Boleto[] }>;
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
  registrar: (accion: string, detalle: string) => void;
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
const CLAVE = "cineight-prototipo-v3";
const VERIFICACION_ADMIN_MS = 5 * 60000;

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
  const [adminHasta, setAdminHasta] = useState(0);
  const [loginAbierto, setLoginAbierto] = useState(false);
  const [loginUsado, setLoginUsado] = useState(false);
  const [modoLogin, setModoLogin] = useState<"login" | "registro">("login");
  const [triviaAbierta, setTriviaAbierta] = useState(false);
  const [triviaUsada, setTriviaUsada] = useState(false);
  const [reloj, setReloj] = useState(0);

  // Estado que viene de la API (no se guarda en localStorage).
  const [modoApi, setModoApi] = useState<ModoApi>(apiConfigurada ? "verificando" : "sin-conexion");
  const [intento, setIntento] = useState(0);
  const [funcionesApi, setFuncionesApi] = useState<Funcion[]>([]);
  const [peliculasApi, setPeliculasApi] = useState<Pelicula[]>([]);
  const [asientosApi, setAsientosApi] = useState<Record<string, AsientosApi>>({});
  const [sesionApi, setSesionApi] = useState<{ correo: string; id: number } | null>(null);
  const [cuentaApi, setCuentaApi] = useState<CuentaApi | null>(null);

  useEffect(() => {
    const t = setInterval(() => setReloj(Date.now()), 15000);
    return () => clearInterval(t);
  }, []);
  const ref = useRef(e);
  const sesionRef = useRef(sesionApi);
  useLayoutEffect(() => {
    ref.current = e;
    sesionRef.current = sesionApi;
  }, [e, sesionApi]);

  useEffect(() => {
    try {
      const raw = localStorage.getItem(CLAVE);
      // Hidratación desde localStorage: solo existe en el navegador, por eso se lee tras montar.
      const previo: Estado = raw ? { ...INICIAL, ...JSON.parse(raw) } : INICIAL;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setE({ usuario: previo.usuario, reserva: previo.reserva, bitacora: previo.bitacora, cuentas: conPersonal(previo.cuentas) });
    } catch {
      setE({ ...INICIAL, cuentas: conPersonal({}) });
    }
    setListo(true);
  }, []);

  useEffect(() => {
    if (!listo) return;
    try {
      localStorage.setItem(CLAVE, JSON.stringify(e));
    } catch {}
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

  // Una sola solicitud de sesión en vuelo por correo: el pase anti-bots es de un solo uso, así que dos llamadas
  // simultáneas (la del efecto de abajo y la de asegurarSesion al bloquear una butaca) no deben competir por él.
  const vinculando = useRef<{ correo: string; p: Promise<SesionApi> } | null>(null);
  const vincular = useCallback((correo: string, nombre: string) => {
    if (vinculando.current?.correo === correo) return vinculando.current.p;
    const p = api
      .sesion(correo, nombre, paseVigente())
      .then((r) => {
        olvidarPase();
        return r;
      })
      .finally(() => {
        if (vinculando.current?.p === p) vinculando.current = null;
      });
    vinculando.current = { correo, p };
    return p;
  }, []);

  // Vincula al cliente con su cuenta de créditos en PostgreSQL y carga su saldo.
  // Termina sólo cuando hay sesión Y cuenta: si asegurarSesion (p. ej. al bloquear una butaca justo después de
  // iniciar sesión) fijó la sesión primero, esta carga se reintenta en lugar de quedarse con el saldo vacío.
  const correoCliente = e.usuario?.rol === "cliente" ? e.usuario.correo : null;
  const nombreCliente = e.usuario?.nombre ?? "";
  const cuentaCargada = cuentaApi !== null;
  useEffect(() => {
    if (modoApi !== "conectado" || !correoCliente || (sesionApi?.correo === correoCliente && cuentaCargada)) return;
    let vivo = true;
    vincular(correoCliente, nombreCliente)
      .then(async (s) => {
        const c = await api.cuenta(s.usuario_id);
        if (!vivo) return;
        setSesionApi({ correo: correoCliente, id: s.usuario_id });
        setCuentaApi(c);
      })
      .catch((err) => vivo && toast(`No se pudo cargar tu cuenta: ${mensaje(err)}`, "error"));
    return () => {
      vivo = false;
    };
  }, [modoApi, correoCliente, nombreCliente, sesionApi, cuentaCargada, toast, vincular]);

  const asegurarSesion = useCallback(async () => {
    const u = ref.current.usuario;
    if (!u) throw new ApiError(401, "Debes iniciar sesión.");
    const s = sesionRef.current;
    if (s?.correo === u.correo) return s.id;
    const r = await vincular(u.correo, u.nombre);
    setSesionApi({ correo: u.correo, id: r.usuario_id });
    return r.usuario_id;
  }, [vincular]);

  const refrescarCuenta = useCallback(async () => {
    const s = sesionRef.current;
    if (!s) return;
    try {
      setCuentaApi(await api.cuenta(s.id));
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

  const bloquearAsiento = useCallback(
    async (funcionId: string, asiento: string): Promise<Resultado> => {
      if (!ref.current.usuario) return { ok: false, error: "Debes iniciar sesión para elegir butacas." };
      try {
        const id = await asegurarSesion();
        await api.bloquearAsiento(funcionId, asiento, id);
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    [asegurarSesion],
  );

  const liberarAsientos = useCallback((funcionId: string, asientos: string[]) => {
    const s = sesionRef.current;
    if (!s || asientos.length === 0) return;
    api.desbloquearAsientos(funcionId, asientos, s.id).catch(() => {});
  }, []);

  const misBloqueos = useCallback(
    async (funcionId: string) => {
      if (ref.current.usuario?.rol !== "cliente") return [];
      try {
        const id = await asegurarSesion();
        return (await api.asientos(funcionId, id)).propios ?? [];
      } catch {
        return [];
      }
    },
    [asegurarSesion],
  );

  const registrar = useCallback((accion: string, detalle: string) => setE((s) => ({ ...s, bitacora: log(s, accion, detalle) })), []);

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

  const cancelarReserva = useCallback((motivo: "cancelada" | "expirada" = "cancelada") => {
    const r = ref.current.reserva;
    if (!r) return;
    // Al cancelar se liberan las butacas en Redis; al expirar, el TTL ya lo hizo solo.
    if (motivo === "cancelada") api.cancelarReserva(r.id).catch(() => {});
    setE((s) => ({ ...s, reserva: null, bitacora: log(s, motivo === "expirada" ? "RESERVA_EXPIRADA" : "RESERVA_CANCELADA", `${r.id} · ${r.asientos.join(", ")}`) }));
    if (motivo === "expirada")
      setAviso({
        icono: Timer,
        titulo: "Tiempo agotado",
        texto: `Tu reserva de ${r.asientos.join(", ")} venció a los ${PARAMETROS.retencionMinutos} minutos y los asientos fueron liberados. No se cobró nada.`,
      });
  }, []);

  // Temporizador de expiración (Redis libera las butacas con el TTL; aquí sólo se avisa al cliente)
  useEffect(() => {
    if (!e.reserva) return;
    const ms = e.reserva.expira - Date.now();
    const t = setTimeout(() => cancelarReserva("expirada"), Math.max(0, ms));
    return () => clearTimeout(t);
  }, [e.reserva, cancelarReserva]);

  // Cierre de sesión automático por inactividad (RNF-24).
  const ultimaActividad = useRef(0);
  useEffect(() => {
    ultimaActividad.current = Date.now();
    const marcar = () => (ultimaActividad.current = Date.now());
    const eventos = ["mousemove", "keydown", "pointerdown", "scroll", "touchstart"];
    eventos.forEach((ev) => window.addEventListener(ev, marcar, { passive: true }));
    const t = setInterval(() => {
      if (ref.current.usuario && Date.now() - ultimaActividad.current > PARAMETROS.inactividadMinutos * 60000) {
        cancelarReserva();
        setE((s) => ({ ...s, usuario: null, bitacora: log(s, "SESION_EXPIRADA", "Inactividad") }));
        setSesionApi(null);
        setCuentaApi(null);
        setTriviaAbierta(false);
        setAviso({
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
  }, [cancelarReserva]);

  const reservaPropia = e.reserva && e.reserva.titular === e.usuario?.correo ? e.reserva : null;
  const cuentaCliente = e.usuario?.rol === "cliente" && sesionApi?.correo === e.usuario.correo ? cuentaApi : null;

  const valor: Ctx = {
    listo,
    modoApi,
    usuario: e.usuario,
    // Mientras la cuenta del cliente se carga no se muestra un "0 créditos" provisional (heurística 1 de Nielsen).
    saldoListo: e.usuario?.rol !== "cliente" || cuentaCliente !== null,
    saldo: cuentaCliente?.saldo ?? 0,
    movimientos: cuentaCliente?.movimientos.map(movimientoDeApi) ?? [],
    boletos: sinDuplicados(cuentaCliente?.boletos.map((b) => boletoDeApi(b, e.usuario!.correo)) ?? []),
    cuentas: e.cuentas,
    reserva: reservaPropia,
    bitacora: e.bitacora,
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
    registrar,
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
    iniciarSesion: (correo, clave) => {
      const c = correo.trim().toLowerCase();
      const cta = ref.current.cuentas[c];
      if (!cta || cta.claveHash !== hashClave(c, clave)) {
        setE((s) => ({ ...s, bitacora: log(s, "INICIO_FALLIDO", c) }));
        return { ok: false, error: "Datos incorrectos." };
      }
      if (!cta.activo) return { ok: false, error: "Esta cuenta fue dada de baja. Contacta al administrador." };
      const u = { nombre: cta.nombre, correo: c, rol: cta.rol };
      setE((s) => ({ ...s, usuario: u, bitacora: log({ ...s, usuario: u }, "INICIO_SESION", `Rol: ${u.rol}`) }));
      setAdminHasta(0);
      toast(`Bienvenido, ${u.nombre}`, "ok");
      return { ok: true };
    },
    registrarCliente: (nombre, correo, clave) => {
      // Mismas reglas que el formulario (defensa en profundidad): nombre real, correo válido y contraseña segura.
      const c = normalizarCorreo(correo);
      const invalido = validarNombre(nombre) ?? validarCorreo(c) ?? validarClaveNueva(clave, c);
      if (invalido) return { ok: false, error: invalido };
      if (ref.current.cuentas[c]) return { ok: false, error: "El correo ya está registrado." };
      const u = { nombre: normalizarNombre(nombre), correo: c, rol: "cliente" as Rol };
      setE((s) => ({ ...s, usuario: u, cuentas: { ...s.cuentas, [c]: cuentaNueva(u.nombre, c, "cliente", clave) }, bitacora: log({ ...s, usuario: u }, "CUENTA_CREADA", "Registro de cliente") }));
      return { ok: true };
    },
    adminVerificado: adminHasta > reloj,
    verificarAdmin: (clave) => {
      const s = ref.current;
      const u = s.usuario;
      const ok = !!u && u.rol === "admin" && s.cuentas[u.correo]?.claveHash === hashClave(u.correo, clave);
      setE((st) => ({ ...st, bitacora: log(st, ok ? "REAUTENTICACION_ADMIN" : "REAUTENTICACION_FALLIDA", "Gestión de usuarios") }));
      if (ok) {
        const n = Date.now();
        setReloj(n);
        setAdminHasta(n + VERIFICACION_ADMIN_MS);
      }
      return ok;
    },
    crearUsuario: ({ nombre, correo, rol, clave }) => {
      const s = ref.current;
      if (s.usuario?.rol !== "admin" || adminHasta < Date.now()) return { ok: false, error: "Acción no autorizada: reconfirma tu identidad de administrador." };
      const c = normalizarCorreo(correo);
      const invalido = validarNombre(nombre) ?? validarCorreo(c);
      if (invalido) return { ok: false, error: invalido };
      if (s.cuentas[c]) return { ok: false, error: "Ya existe una cuenta con ese correo." };
      const claveInvalida = validarClaveNueva(clave, c);
      if (claveInvalida) return { ok: false, error: claveInvalida.replace("La contraseña", "La contraseña temporal") };
      setE((st) => ({ ...st, cuentas: { ...st.cuentas, [c]: cuentaNueva(normalizarNombre(nombre), c, rol, clave) }, bitacora: log(st, "USUARIO_CREADO", `${c} · rol ${rol}`) }));
      return { ok: true };
    },
    editarUsuario: (correo, { nombre, rol }) => {
      const s = ref.current;
      if (s.usuario?.rol !== "admin" || adminHasta < Date.now()) return { ok: false, error: "Acción no autorizada: reconfirma tu identidad de administrador." };
      const c = s.cuentas[correo];
      if (!c) return { ok: false, error: "La cuenta no existe." };
      const nombreInvalido = validarNombre(nombre);
      if (nombreInvalido) return { ok: false, error: nombreInvalido };
      nombre = normalizarNombre(nombre);
      if (correo === s.usuario.correo && rol !== c.rol) return { ok: false, error: "No puedes cambiar tu propio rol." };
      if (c.rol === "admin" && rol !== "admin" && c.activo && adminsActivos(s.cuentas) <= 1) return { ok: false, error: "Debe existir al menos un administrador activo." };
      setE((st) => ({
        ...st,
        cuentas: conCuenta(st, correo, (x) => ({ ...x, nombre: nombre.trim(), rol })),
        bitacora: log(st, "USUARIO_EDITADO", `${correo} · ${c.rol} → ${rol}${c.nombre !== nombre.trim() ? ` · nombre: ${c.nombre} → ${nombre.trim()}` : ""}`),
      }));
      return { ok: true };
    },
    cambiarEstadoUsuario: (correo, activo) => {
      const s = ref.current;
      if (s.usuario?.rol !== "admin" || adminHasta < Date.now()) return { ok: false, error: "Acción no autorizada: reconfirma tu identidad de administrador." };
      const c = s.cuentas[correo];
      if (!c) return { ok: false, error: "La cuenta no existe." };
      if (!activo && correo === s.usuario.correo) return { ok: false, error: "No puedes dar de baja tu propia cuenta." };
      if (!activo && c.rol === "admin" && adminsActivos(s.cuentas) <= 1) return { ok: false, error: "Debe existir al menos un administrador activo." };
      setE((st) => ({ ...st, cuentas: conCuenta(st, correo, (x) => ({ ...x, activo })), bitacora: log(st, activo ? "USUARIO_REACTIVADO" : "USUARIO_DADO_DE_BAJA", correo) }));
      return { ok: true };
    },
    logout: () => {
      cancelarReserva();
      setAdminHasta(0);
      setSesionApi(null);
      setCuentaApi(null);
      setTriviaAbierta(false);
      setLoginAbierto(false);
      limpiarTurnos();
      setE((s) => ({ ...s, usuario: null, bitacora: log(s, "CIERRE_SESION", "") }));
    },
    reservar: async (funcionId, asientos) => {
      const s = ref.current;
      if (!s.usuario) return { ok: false, error: "Debes iniciar sesión para reservar asientos." };
      if (s.reserva && s.reserva.titular === s.usuario.correo) return { ok: false, error: "Ya tienes una reserva activa." };
      if (asientos.length === 0) return { ok: false, error: "Selecciona al menos una butaca." };
      if (asientos.length > PARAMETROS.maxButacasPorTransaccion) return { ok: false, error: `Máximo ${PARAMETROS.maxButacasPorTransaccion} butacas por transacción.` };
      const titular = s.usuario.correo;
      // POST /api/reservas: SET NX + TTL en Redis. El servidor decide si las butacas siguen libres
      // y aplica el límite de boletos del preestreno.
      try {
        const id = await asegurarSesion();
        const res = await api.crearReserva(funcionId, id, asientos);
        // El vencimiento se cuenta con el reloj local para no depender del desfase entre equipos.
        const r: Reserva = { id: res.reserva_id, titular, funcionId, asientos, expira: Date.now() + PARAMETROS.retencionMinutos * 60000 - 1000 };
        setE((st) => ({ ...st, reserva: r, bitacora: log(st, "RESERVA_CREADA", `${r.id} · ${asientos.join(", ")}`) }));
        return { ok: true };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      } finally {
        cargarOcupacion([funcionId]);
      }
    },
    cancelarReserva,
    pagar: async () => {
      const s = ref.current;
      const r = s.reserva;
      if (!r || !s.usuario || r.titular !== s.usuario.correo) return { ok: false, error: "La reserva expiró." };
      const f = funciones.find((x) => x.id === r.funcionId)!;
      const total = f.precio * r.asientos.length;
      // POST /api/reservas/{id}/pagar: transacción con SELECT … FOR UPDATE; repetirla no vuelve a cobrar.
      try {
        const p = await api.pagar(r.id);
        const nuevos = p.boletos.map((b) => boletoDeApi(b, r.titular));
        setE((st) => ({ ...st, reserva: null, bitacora: log(st, "VENTA", `${r.id} · ${r.asientos.join(", ")} · ${total} cr.`) }));
        await refrescarCuenta();
        cargarOcupacion([r.funcionId]);
        return { ok: true, boletos: nuevos };
      } catch (err) {
        const status = err instanceof ApiError ? err.status : 0;
        if (status === 410 || status === 409) setE((st) => ({ ...st, reserva: null }));
        if (status === 402) {
          await refrescarCuenta();
          return { ok: false, error: `Saldo insuficiente: necesitas ${total} créditos y tienes ${cuentaApi?.saldo ?? 0}.` };
        }
        return { ok: false, error: mensaje(err) };
      }
    },
    colaEntrar: async (eventoId) => {
      try {
        const id = await asegurarSesion();
        const pos = await api.entrarCola(eventoId, id, paseVigente());
        olvidarPase();
        setE((st) => ({ ...st, bitacora: log(st, "COLA_INGRESO", `${eventoId} · ${pos.turno_habilitado ? "turno inmediato" : `posición ${pos.posicion}`}`) }));
        return { ok: true, pos };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    colaConsultar: async (eventoId) => {
      try {
        const id = await asegurarSesion();
        return { ok: true, pos: await api.posicionCola(eventoId, id) };
      } catch (err) {
        return { ok: false, error: mensaje(err), fuera: err instanceof ApiError && err.status === 404 };
      }
    },
    colaSalir: (eventoId) => {
      const s = sesionRef.current;
      if (s) api.salirCola(eventoId, s.id).catch(() => {});
    },
    acreditarTrivia: async (partidaId, aciertos) => {
      if (!ref.current.usuario) return { ok: false, monto: 0, error: "Debes iniciar sesión." };
      // El servidor calcula el premio a partir de los aciertos y no acredita dos veces la misma partida (RF-64).
      try {
        const id = await asegurarSesion();
        const r = await api.acreditarTrivia(id, partidaId, aciertos);
        await refrescarCuenta();
        setE((st) => ({ ...st, bitacora: log(st, "TRIVIA", `${partidaId} · +${r.acreditado ? r.monto : 0} cr.`) }));
        return { ok: r.acreditado, monto: r.monto };
      } catch (err) {
        return { ok: false, monto: 0, error: mensaje(err) };
      }
    },
    validarBoleto: async (entrada) => {
      // Acepta el contenido del QR (JSON con el código dinámico y su ventana) o "TOKEN CÓDIGO" escrito a mano.
      // El servidor comprueba el código: un QR vencido (captura de pantalla) o sin código no consume el boleto.
      let tok = "";
      let codigo = "";
      let ventana = ventanaActual();
      const texto = entrada.trim();
      if (texto.startsWith("{")) {
        try {
          const q = JSON.parse(texto);
          tok = String(q.t ?? "");
          codigo = String(q.c ?? "");
          ventana = Number(q.w);
        } catch {}
      } else {
        [tok = "", codigo = ""] = texto.split(/\s+/);
        // A mano no se conoce la ventana: se busca la del minuto anterior, el actual o el siguiente que dé ese código.
        const w = ventanaActual();
        ventana = [w, w - 1, w + 1].find((x) => codigoDinamico(tok, x) === codigo.toUpperCase()) ?? w;
      }
      let res: Validacion;
      if (!tok) res = { ok: false, motivo: "Boleto inexistente" };
      else if (!codigo) res = { ok: false, motivo: "Falta el código de 6 caracteres que aparece bajo el QR." };
      else {
        try {
          const r = await api.validarBoleto(tok, codigo, ventana);
          res = { ok: r.ok, motivo: r.motivo, boleto: r.boleto && boletoDeApi(r.boleto, r.titular ?? "") };
        } catch (err) {
          res = { ok: false, motivo: mensaje(err) };
        }
      }
      setE((st) => ({ ...st, bitacora: log(st, "VALIDACION_ACCESO", `${tok.slice(0, 6)}… · ${res.motivo}`) }));
      return res;
    },
    peliculas,
    crearPelicula: async (p, nuevas) => {
      const s = ref.current;
      if (s.usuario?.rol !== "agente" && s.usuario?.rol !== "admin") return { ok: false, error: "Sólo el agente operativo o el administrador pueden crear películas." };
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
          creada_por: s.usuario.correo,
        });
        await recargarCatalogo();
        setE((st) => ({ ...st, bitacora: log(st, "PELICULA_CREADA", `${r.slug} · ${r.funciones.length} funciones`) }));
        return { ok: true, id: r.slug, funciones: r.funciones.length };
      } catch (err) {
        return { ok: false, error: mensaje(err) };
      }
    },
    programarFuncion: async ({ peliculaId, ...f }) => {
      try {
        const r = await api.programarFuncion(peliculaId, { sala: f.salaId, inicio: f.inicio, formato: f.formato, idioma: f.idioma, precio: f.precio });
        await recargarCatalogo();
        setE((st) => ({ ...st, bitacora: log(st, "FUNCION_PROGRAMADA", `${r.codigo} · ${f.inicio}`) }));
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
          {!listo || modoApi === "verificando" ? (
            <div className="py-24 text-center text-sm text-tenue">
              {listo && !apiLocal ? "Conectando con el servidor… la primera visita puede tardar hasta un minuto." : "Cargando…"}
            </div>
          ) : modoApi === "sin-conexion" ? (
            <ServicioNoDisponible configurada={apiConfigurada} onReintentar={reintentarConexion} />
          ) : (
            children
          )}
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
