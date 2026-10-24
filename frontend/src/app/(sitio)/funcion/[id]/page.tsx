"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { m } from "framer-motion";
import { CalendarRange, CircleCheck, Clock, CreditCard, Gamepad2, Gem, ListX, LogIn, Ticket, Timer, Users, X } from "lucide-react";
import { pelicula, sala, finFuncion } from "@/data/catalogo";
import { PARAMETROS } from "@/data/parametros";
import { fmtDuracion, fmtFechaLarga, fmtHora } from "@/utils/formato";
import { otorgarTurno, quitarTurno, turnoVigente } from "@/utils/turnoCola";
import { useApp } from "@/store/AppProvider";
import type { Boleto } from "@/types/cuenta";
import type { EventoAsientosApi } from "@/types/api";
import { useAsientosEnVivo } from "@/hooks/useAsientosEnVivo";
import { Poster } from "@/components/ui/Poster";
import { Modal } from "@/components/ui/Modal";
import { BotonVolver } from "@/components/ui/BotonVolver";
import { Boton, BotonEnlace } from "@/components/ui/Boton";
import { GrupoBoletos } from "@/components/boletos/Boleto";
import { MapaButacas, type EstadoButaca } from "@/components/funcion/MapaButacas";
import { CuentaReserva, DesgloseTotal, PasosCompra, Reloj } from "@/components/funcion/PiezasCompra";

type Alerta = { titulo: string; texto: string };

/**
 * Selección de butacas y compra.
 *
 * Concurrencia: cada clic en una butaca libre pide un candado atómico en Redis (SET NX EX 300). Si otro cliente
 * la tomó primero, el servidor responde 409 y la butaca no entra en la selección. Los cambios de todos los clientes
 * llegan por SSE (useAsientosEnVivo), así el mapa muestra al instante lo que bloquean o compran los demás.
 */
export default function PaginaFuncion() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const app = useApp();
  const { funciones, ocupacion, cargarOcupacion, usuario, reserva, saldo, listo } = app;
  const f = funciones.find((x) => x.id === id);
  const [sel, setSel] = useState<string[]>([]);
  const [pendientes, setPendientes] = useState<string[]>([]);
  const [checkout, setCheckout] = useState(false);
  const [errorPago, setErrorPago] = useState("");
  const [procesando, setProcesando] = useState(false);
  const [alerta, setAlerta] = useState<Alerta | null>(null);
  const [confirmarCancelar, setConfirmarCancelar] = useState(false);
  const [emitidos, setEmitidos] = useState<Boleto[] | null>(null);

  // Referencias al estado más reciente: permiten handlers estables (las 192 butacas están memoizadas).
  const appRef = useRef(app);
  const selRef = useRef(sel);
  const pendientesRef = useRef(pendientes);
  const bloqueadoEn = useRef(new Map<string, number>());
  // Butaca que tocó un visitante sin sesión: se aparta en cuanto inicia sesión.
  const butacaPendiente = useRef<string | null>(null);
  useLayoutEffect(() => {
    appRef.current = app;
    selRef.current = sel;
    pendientesRef.current = pendientes;
  });

  const fid = f?.id;
  const limite = f?.eventoId ? PARAMETROS.maxBoletosPorEvento : PARAMETROS.maxButacasPorTransaccion;

  // Foto inicial de la ocupación; después la mantiene al día el stream SSE.
  useEffect(() => {
    if (fid) cargarOcupacion([fid]);
  }, [fid, cargarOcupacion]);

  // Al entrar (o recargar) se recuperan las butacas que este cliente ya tenía bloqueadas: siguen siendo suyas.
  const correo = usuario?.correo;
  const conectado = app.modoApi === "conectado";
  useEffect(() => {
    if (!fid || !correo || !conectado) return;
    let vivo = true;
    appRef.current.misBloqueos(fid).then((propios) => {
      if (!vivo || !propios.length) return;
      const ahora = Date.now();
      propios.forEach((a) => bloqueadoEn.current.set(a, ahora));
      setSel((x) => [...new Set([...x, ...propios])]);
    });
    return () => {
      vivo = false;
    };
  }, [fid, correo, conectado]);

  // Cambios de otros clientes: si vence o se pierde un bloqueo propio, la butaca sale de la selección.
  const alEvento = useCallback((ev: EventoAsientosApi) => {
    if (ev.tipo === "bloqueado") return;
    const ahora = Date.now();
    const definitiva = ev.tipo === "vendido" || ev.tipo === "retirado";
    const perdidas = ev.asientos.filter((a) => selRef.current.includes(a) && (definitiva || ahora - (bloqueadoEn.current.get(a) ?? 0) > 3000));
    if (!perdidas.length) return;
    setSel((x) => x.filter((a) => !perdidas.includes(a)));
    appRef.current.toast(
      ev.tipo === "vendido"
        ? `La butaca ${perdidas.join(", ")} fue vendida.`
        : ev.tipo === "retirado"
          ? `La butaca ${perdidas.join(", ")} quedó fuera de servicio.`
          : `Tu bloqueo de ${perdidas.join(", ")} venció tras ${PARAMETROS.retencionMinutos} min. Vuelve a elegirla si sigue libre.`,
      "info",
    );
  }, []);
  const conexion = useAsientosEnVivo(fid, alEvento);

  // Al navegar a otra página de la app se sueltan las butacas seleccionadas que no se reservaron.
  // Al recargar o cerrar la pestaña NO se sueltan: al volver se recuperan (misBloqueos) y, si el cliente no vuelve,
  // el TTL de Redis las libera a los 5 minutos. Así una recarga no hace perder la selección.
  useEffect(() => {
    if (!fid) return;
    return () => {
      if (selRef.current.length) appRef.current.liberarAsientos(fid, selRef.current);
    };
  }, [fid]);

  // Depende sólo del id de la ruta (primitivo) para que el handler sea estable y las butacas memoizadas no se repinten.
  const elegir = useCallback(
    async (a: string) => {
      const ctx = appRef.current;
      const fn = ctx.funciones.find((x) => x.id === id);
      if (!fn) return;
      const fid = fn.id;
      const limite = fn.eventoId ? PARAMETROS.maxBoletosPorEvento : PARAMETROS.maxButacasPorTransaccion;
      if (ctx.reserva?.funcionId === fid) return setCheckout(true);
      if (!ctx.usuario) {
        butacaPendiente.current = a;
        ctx.toast(`Inicia sesión y apartaremos la butaca ${a} a tu nombre durante ${PARAMETROS.retencionMinutos} minutos.`, "info");
        return ctx.abrirLogin();
      }
      if (selRef.current.includes(a)) {
        setSel((x) => x.filter((y) => y !== a));
        ctx.liberarAsientos(fid, [a]);
        return;
      }
      if (selRef.current.length + pendientesRef.current.length >= limite)
        return setAlerta({ titulo: "Límite de butacas", texto: `Puedes seleccionar hasta ${limite} butacas en esta función.` });

      setPendientes((x) => [...x, a]);
      const r = await ctx.bloquearAsiento(fid, a);
      setPendientes((x) => x.filter((y) => y !== a));
      if (!r.ok) {
        setAlerta({ titulo: "Butaca no disponible", texto: r.error! });
        ctx.cargarOcupacion([fid]);
        return;
      }
      bloqueadoEn.current.set(a, Date.now());
      setSel((x) => (x.includes(a) ? x : [...x, a]));
      // Preestreno: bloquear una butaca confirma el turno y el servidor lo extiende a su duración completa.
      const evento = fn.eventoId;
      if (evento)
        ctx.colaConsultar(evento).then((q) => {
          if (q.ok && q.pos?.turno_habilitado && q.pos.expira) otorgarTurno(evento, Date.parse(q.pos.expira));
        });
    },
    [id, setCheckout, setAlerta],
  );

  useEffect(() => {
    const a = butacaPendiente.current;
    if (!correo || !a) return;
    butacaPendiente.current = null;
    elegir(a);
  }, [correo, elegir]);

  if (!f)
    return (
      <div className="flex items-center gap-4">
        <BotonVolver />
        <p className="text-tenue">La función no existe.</p>
      </div>
    );
  const p = pelicula(f.peliculaId);
  const s = sala(f.salaId);
  if (new Date(f.inicio) <= new Date() && !emitidos)
    return (
      <div className="tarjeta mx-auto max-w-md p-8 text-center">
        <Clock aria-hidden className="mx-auto h-10 w-10 text-arena" />
        <p className="titulo mt-3 text-2xl">Esta función ya comenzó</p>
        <p className="mt-2 text-sm text-tenue">
          {p.titulo} · {fmtFechaLarga(f.inicio)} a las {fmtHora(f.inicio)}. Elige otro horario para comprar tus boletos.
        </p>
        <BotonEnlace href={`/pelicula/${p.id}#horarios`} icono={CalendarRange} className="mt-5">
          Ver horarios
        </BotonEnlace>
      </div>
    );
  const occ = ocupacion(f.id);
  const miReserva = reserva?.funcionId === f.id ? reserva : null;
  const asientosResumen = miReserva?.asientos ?? sel;
  const total = asientosResumen.length * f.precio;
  const faltan = usuario?.rol === "cliente" ? Math.max(0, total - saldo) : 0;
  const sinTurno = listo && f.eventoId && !turnoVigente(f.eventoId) && !miReserva && !emitidos;

  const estadoDe = (a: string): EstadoButaca =>
    miReserva?.asientos.includes(a)
      ? "mio"
      : occ.fuera.has(a)
        ? "fuera"
        : occ.vendidos.has(a)
        ? "vendida"
        : pendientes.includes(a)
          ? "pendiente"
          : sel.includes(a)
            ? "sel"
            : occ.retenidos.has(a)
              ? "bloqueada"
              : "libre";

  const limpiarSeleccion = () => {
    app.liberarAsientos(f.id, sel);
    setSel([]);
  };

  const reservar = async () => {
    if (!usuario) return app.abrirLogin();
    if (procesando) return;
    setProcesando(true);
    const r = await app.reservar(f.id, sel);
    setProcesando(false);
    if (!r.ok) {
      setAlerta({ titulo: "No pudimos reservar", texto: r.error! });
      if (/ya no está|vendid|tomada/.test(r.error!)) limpiarSeleccion();
      return;
    }
    setSel([]); // las butacas ya pertenecen a la reserva
    setErrorPago("");
    setCheckout(true);
  };

  const pagar = async () => {
    if (procesando) return;
    setProcesando(true);
    const r = await app.pagar();
    setProcesando(false);
    if (!r.ok) return setErrorPago(r.error!);
    setCheckout(false);
    setEmitidos(r.boletos!);
    // Preestreno: la compra termina el turno y el cupo pasa al siguiente de la cola virtual.
    if (f.eventoId) {
      app.colaSalir(f.eventoId);
      quitarTurno(f.eventoId);
    }
  };

  // La trivia se abre encima de esta ventana: al volver, la reserva y las butacas siguen igual y el saldo ya está actualizado.
  const jugarTrivia = () => {
    setErrorPago("");
    app.abrirTrivia();
  };

  const cancelar = () => {
    app.cancelarReserva();
    setConfirmarCancelar(false);
    setCheckout(false);
  };

  const resumenLateral = (
    <>
      {miReserva ? (
        <CuentaReserva expira={miReserva.expira} />
      ) : (
        <p className="flex items-start gap-2 rounded-md border border-borde bg-fondo px-3 py-2.5 text-xs text-tenue">
          <Timer aria-hidden className="mt-px h-3.5 w-3.5 shrink-0 text-arena" />
          Cada butaca que eliges queda apartada a tu nombre {PARAMETROS.retencionMinutos} minutos mientras decides.
        </p>
      )}
      <div>
        <p className="etiqueta mb-2">{miReserva ? "Butacas retenidas a tu nombre" : "Tu selección (bloqueada para ti)"}</p>
        {asientosResumen.length === 0 ? (
          <p className="text-xs text-tenue">Elige hasta {limite} butacas en el mapa.</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {asientosResumen.map((a) => (
              <span key={a} className="rounded border border-borde bg-panel-2 px-2 py-1 text-[11px] font-semibold">
                <span className="mr-1 text-salmon">●</span>Fila {a.split("-")[0]} · {a.split("-")[1]}
              </span>
            ))}
          </div>
        )}
      </div>
      <div className="flex items-end justify-between border-t border-borde pt-3">
        <span>
          <span className="etiqueta block">
            Total ({asientosResumen.length} boleto{asientosResumen.length === 1 ? "" : "s"})
          </span>
          <span className="text-2xl font-black text-oro">{total} créditos</span>
        </span>
        {usuario?.rol === "cliente" && (
          <span className="flex items-center gap-1 text-[11px] text-tenue">
            Saldo: <Gem aria-hidden className="h-3 w-3 text-oro" /> {saldo}
          </span>
        )}
      </div>
      {faltan > 0 && <AvisoSaldo faltan={faltan} onTrivia={jugarTrivia} />}
      {miReserva ? (
        <div className="grid gap-2">
          <Boton icono={CreditCard} tamano="lg" onClick={() => setCheckout(true)} className="w-full">
            Continuar con el pago
          </Boton>
          <Boton variante="borde" icono={X} onClick={() => setConfirmarCancelar(true)} className="w-full">
            Cancelar reserva
          </Boton>
        </div>
      ) : (
        <div className="grid gap-2">
          <Boton
            icono={usuario ? Ticket : LogIn}
            tamano="lg"
            cargando={procesando}
            disabled={(usuario && !sel.length) || (!!reserva && !miReserva)}
            onClick={reservar}
            className="w-full"
          >
            {!usuario ? "Iniciar sesión" : procesando ? "Reservando…" : `Reservar · ${total} cr.`}
          </Boton>
          {usuario && (
            <Boton variante="borde" icono={ListX} onClick={limpiarSeleccion} disabled={!sel.length} className="w-full">
              Soltar selección
            </Boton>
          )}
          {!usuario && <p className="text-center text-[11px] text-tenue">Las butacas se bloquean a tu nombre al elegirlas, por eso necesitas una sesión.</p>}
        </div>
      )}
      {reserva && !miReserva && <p className="text-[11px] text-tenue">Ya tienes una reserva activa en otra función. Termínala o cancélala primero.</p>}
    </>
  );

  return (
    <div className="pb-20 lg:pb-0">
      <div className="mb-5">
        <PasosCompra actual={miReserva ? 3 : 2} />
      </div>
      {/* Encabezado de la función */}
      <div className="flex items-start justify-between gap-3 sm:gap-4">
        <div className="flex min-w-0 flex-1 items-start gap-3 sm:gap-4">
          <BotonVolver href={`/pelicula/${p.id}`} texto={`Volver a ${p.titulo}`} className="mt-1 shrink-0" />
          <div className="min-w-0">
            <div className="flex flex-wrap gap-2">
              <span className="badge-vino">{f.formato}</span>
              {s.tipo !== f.formato && <span className="badge-borde">{s.tipo}</span>}
              <span className="badge-borde">{f.idioma}</span>
              <span className="badge-borde">{p.clasificacion}</span>
            </div>
            <h1 className="titulo mt-3 text-2xl leading-tight sm:text-3xl xl:text-4xl">
              {p.titulo} <span className="text-apagado">/</span> {s.nombre} {s.tipo}
            </h1>
            <p className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-tenue">
              <span className="first-letter:uppercase">{fmtFechaLarga(f.inicio)}</span>
              <span className="flex items-center gap-1.5">
                <Clock aria-hidden className="h-3.5 w-3.5" />
                <b className="text-tinta">{fmtHora(f.inicio)} hrs</b> – {fmtHora(finFuncion(f))}
              </span>
              <span className="font-semibold text-oro">{fmtDuracion(p.duracion)}</span>
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <Poster p={p} className="hidden aspect-[2/3] w-14 sm:block" rounded="rounded-md" sizes="56px" />
          <Link
            href={`/pelicula/${p.id}#horarios`}
            aria-label="Cambiar de función"
            title="Cambiar de función"
            className="grid h-10 w-10 place-items-center rounded-md border border-borde bg-panel-2 text-tinta transition-colors hover:border-arena hover:text-oro"
          >
            <CalendarRange aria-hidden className="h-5 w-5" />
          </Link>
        </div>
      </div>

      {sinTurno ? (
        <div className="tarjeta mt-8 border-rojo/50 bg-vino/30 p-10 text-center">
          <p className="titulo text-lg">Preestreno de {p.titulo}</p>
          <p className="mt-2 text-sm text-tenue">Por la alta demanda, para elegir asientos necesitas un turno vigente de la cola virtual.</p>
          <BotonEnlace href={`/cola/${f.eventoId}`} icono={Users} className="mt-5">
            Ir a la cola virtual
          </BotonEnlace>
        </div>
      ) : (
        <div className="mt-6 grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
          <MapaButacas sala={s} estadoDe={estadoDe} resaltadas={asientosResumen} onElegir={elegir} conexion={conexion} />

          {/* Resumen (RF-21): panel lateral fijo arriba en escritorio */}
          <aside className="tarjeta hidden space-y-4 p-5 lg:sticky lg:top-24 lg:block">{resumenLateral}</aside>
        </div>
      )}

      {/* Barra inferior compacta (sólo pantallas pequeñas) */}
      {!sinTurno && (
        <div className="fixed inset-x-0 bottom-0 z-30 border-t border-borde bg-fondo/95 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden">
          <div className="mx-auto flex max-w-7xl items-center gap-3 px-4 py-2.5">
            {miReserva && (
              <span className="flex items-center gap-1 rounded border border-rojo/60 bg-vino/50 px-2 py-1 text-sm font-black tabular-nums text-salmon">
                <Timer aria-hidden className="h-3.5 w-3.5" /> <Reloj expira={miReserva.expira} />
              </span>
            )}
            <span className="min-w-0 flex-1 leading-tight">
              <span className="etiqueta block truncate">{asientosResumen.length ? asientosResumen.join(", ") : `Hasta ${limite} butacas`}</span>
              <span className="text-base font-black text-oro">{total} créditos</span>
            </span>
            {miReserva ? (
              <Boton icono={CreditCard} onClick={() => setCheckout(true)} className="!px-4">
                Pagar
              </Boton>
            ) : (
              <Boton icono={usuario ? Ticket : LogIn} cargando={procesando} disabled={(usuario && !sel.length) || (!!reserva && !miReserva)} onClick={reservar} className="!px-4">
                {usuario ? "Reservar" : "Entrar"}
              </Boton>
            )}
          </div>
        </div>
      )}

      {/* Ventana emergente de compra */}
      <Modal abierto={checkout && !!miReserva} onCerrar={() => setCheckout(false)} titulo="Finalizar compra" ancho="max-w-lg">
        {miReserva && (
          <div className="space-y-4">
            <CuentaReserva expira={miReserva.expira} />
            <div className="flex gap-3 rounded-lg border border-borde bg-fondo p-3">
              <Poster p={p} className="aspect-[2/3] w-14 shrink-0" rounded="rounded" sizes="56px" />
              <div className="text-sm">
                <p className="titulo">{p.titulo}</p>
                <p className="text-tenue">
                  {s.nombre} · {s.tipo}
                </p>
                <p className="text-tenue first-letter:uppercase">
                  {fmtFechaLarga(f.inicio)} · {fmtHora(f.inicio)}
                </p>
              </div>
            </div>
            <DesgloseTotal asientos={miReserva.asientos} precio={f.precio} total={total} />

            <p className="flex items-center justify-between text-xs text-tenue">
              <span>Tu saldo</span>
              <span className="flex items-center gap-1 font-bold text-tinta">
                <Gem aria-hidden className="h-3 w-3 text-oro" /> {saldo} créditos
              </span>
            </p>
            {faltan > 0 && <AvisoSaldo faltan={faltan} onTrivia={jugarTrivia} />}
            {errorPago && <div className="rounded-md border border-rojo/60 bg-vino/50 p-3 text-sm text-salmon">{errorPago}</div>}

            <Boton icono={CreditCard} tamano="lg" cargando={procesando} disabled={faltan > 0} onClick={pagar} className="w-full">
              {procesando ? "Procesando pago…" : `Pagar ${total} créditos`}
            </Boton>
            <Boton variante="borde" icono={X} onClick={() => setConfirmarCancelar(true)} className="w-full">
              Cancelar reserva
            </Boton>
          </div>
        )}
      </Modal>

      {/* Confirmación de cancelación */}
      <Modal abierto={confirmarCancelar} onCerrar={() => setConfirmarCancelar(false)} titulo="¿Cancelar la reserva?" z="z-[55]">
        <p className="text-sm text-tenue">
          Las butacas {miReserva?.asientos.join(", ")} se liberarán para otros clientes. No se te cobrará nada.
        </p>
        <div className="mt-5 flex gap-2">
          <Boton variante="borde" onClick={() => setConfirmarCancelar(false)} className="flex-1">
            Volver
          </Boton>
          <Boton icono={X} onClick={cancelar} className="flex-1">
            Sí, cancelar
          </Boton>
        </div>
      </Modal>

      {/* Avisos (butaca tomada, límites…) */}
      <Modal abierto={!!alerta} onCerrar={() => setAlerta(null)} titulo={alerta?.titulo}>
        <p className="text-sm text-tenue">{alerta?.texto}</p>
        <Boton onClick={() => setAlerta(null)} className="mt-5 w-full">
          Entendido
        </Boton>
      </Modal>

      {/* Compra exitosa */}
      <Modal abierto={!!emitidos} onCerrar={() => router.push("/mis-entradas")} ancho="max-w-xl" etiqueta="Compra exitosa">
        {emitidos && (
          <m.div initial={{ opacity: 0, scale: 0.97 }} animate={{ opacity: 1, scale: 1 }} className="space-y-4">
            <div className="text-center">
              <CircleCheck aria-hidden className="mx-auto h-14 w-14 text-oro" />
              <h2 className="titulo mt-3 text-2xl">¡Compra exitosa!</h2>
              <p className="text-sm text-tenue">
                Se debitaron {emitidos.length * f.precio} créditos. Enviamos el comprobante a {usuario?.correo}.
              </p>
            </div>
            <GrupoBoletos boletos={emitidos} />
            <div className="flex gap-2">
              <BotonEnlace href="/mis-entradas" icono={Ticket} className="flex-1">
                Mis Entradas
              </BotonEnlace>
              <BotonEnlace href="/" variante="borde" className="flex-1">
                Cartelera
              </BotonEnlace>
            </div>
          </m.div>
        )}
      </Modal>
    </div>
  );
}

/** Saldo insuficiente: se avisa antes de pagar y se ofrece la trivia, que se abre encima sin perder la selección. */
function AvisoSaldo({ faltan, onTrivia }: { faltan: number; onTrivia: () => void }) {
  return (
    <div className="rounded-md border border-oro/30 bg-oro/10 p-3 text-xs text-oro">
      <p>
        Te faltan <b>{faltan} créditos</b>. Gana hasta {PARAMETROS.bonoTriviaPerfecta} en la trivia sin perder tus butacas.
      </p>
      <Boton variante="oro" tamano="sm" icono={Gamepad2} onClick={onTrivia} className="mt-2">
        Jugar la trivia
      </Boton>
    </div>
  );
}
