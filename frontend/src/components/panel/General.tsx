"use client";

import { Armchair, ArrowRight, Download, LayoutDashboard, ScrollText, Ticket, Users } from "lucide-react";
import { Boton } from "@/components/ui/Boton";
import type { IdSeccion } from "./MarcoPanel";
import { useState } from "react";
import { ROL_TXT, SALAS, pelicula, sala } from "@/data/catalogo";
import { fmtFecha, fmtHora } from "@/utils/formato";
import type { Funcion, Rol } from "@/types/catalogo";
import { Modal } from "@/components/ui/Modal";
import { Poster } from "@/components/ui/Poster";
import { useMetricas, useOcupacionVigente, useFilasVentas } from "@/hooks/usePanelDatos";
import { useBitacora, useUsuarios } from "@/hooks/useDatosServidor";
import { descargarCsv, csvVentas } from "@/utils/csv";
import { thCls, Caja, Kpi, Barra } from "@/components/panel/Elementos";
import { MapaSoloLectura } from "@/components/panel/MapaSoloLectura";

/* ─────────── Panel general (RF-56): dashboard en paneles ─────────── */


export function General({ ir }: { ir: (seccion: IdSeccion) => void }) {
  const { usuarios } = useUsuarios();
  const bitacora = useBitacora() ?? [];
  const met = useMetricas();
  useOcupacionVigente();
  const filas = useFilasVentas(met);
  const [mapa, setMapa] = useState<Funcion | null>(null);

  // ── Métricas clave: GET /api/metricas (PostgreSQL y Redis), actualizadas cada 5 s.
  const k = { vendidos: met?.vendidos_hoy ?? 0, ingresos: met?.ingresos_hoy ?? 0, retenidos: met?.asientos_retenidos ?? 0, trivia: met?.creditos_trivia ?? 0, canje: met?.creditos_canjeados ?? 0, cuentas: met?.cuentas ?? 0 };
  const num = (n: number) => (met ? n.toLocaleString("es-GT") : "—");
  const enCola = met ? Object.values(met.colas).reduce((a, n) => a + n, 0) : 0;
  const aforoTotal = SALAS.reduce((a, s) => a + s.aforo, 0);
  const fuente = met ? "Se actualiza cada 5 s" : "Cargando métricas…";

  // ── Usuarios & roles
  const lista = usuarios ?? [];
  const porRol = (["cliente", "agente", "admin", "auditor"] as Rol[]).map((r) => ({
    rol: r,
    activos: lista.filter((c) => c.rol === r && c.activo).length,
    baja: lista.filter((c) => c.rol === r && !c.activo).length,
  }));
  const maxRol = Math.max(...porRol.map((r) => r.activos + r.baja), 1);
  const recientes = [...lista].sort((a, b) => b.creado.localeCompare(a.creado)).slice(0, 4);

  const proximas = filas.filter((r) => new Date(r.f.inicio) > new Date()).sort((a, b) => a.f.inicio.localeCompare(b.f.inicio)).slice(0, 6);

  return (
    <div className="space-y-6">
      {/* 1 · Métricas clave */}
      <Caja icono={LayoutDashboard} titulo="Métricas clave" sub={fuente}>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <Kpi titulo="Boletos vendidos hoy" valor={num(k.vendidos)} detalle={`${enCola} ${enCola === 1 ? "cliente" : "clientes"} en cola virtual`}>
            <Barra pct={(k.vendidos / Math.max(aforoTotal, 1)) * 100} />
          </Kpi>
          <Kpi titulo="Ingresos de hoy" valor={num(k.ingresos)} sufijo="créditos" detalle="Pagos confirmados">
            <Barra pct={(k.vendidos / Math.max(aforoTotal, 1)) * 100} />
          </Kpi>
          <Kpi titulo="Asientos retenidos" valor={num(k.retenidos)} detalle="Se liberan solos a los 5 min">
            <Barra pct={(k.retenidos / aforoTotal) * 100} />
          </Kpi>
          <Kpi titulo="Créditos trivia vs. canje" valor={num(k.trivia)} sufijo={`/ ${num(k.canje)} canjeados`} detalle={`${num(k.cuentas)} cuentas registradas`}>
            <Barra pct={k.canje ? Math.min(100, (k.trivia / Math.max(k.canje, 1)) * 100) : 0} />
          </Kpi>
        </div>
      </Caja>

      <div className="grid gap-6 xl:grid-cols-2">
        {/* 2 · Gestión de usuarios & roles */}
        <Caja
          icono={Users}
          titulo="Gestión de usuarios & roles"
          sub={`${lista.filter((c) => c.activo).length} cuentas activas · ${lista.filter((c) => !c.activo).length} dadas de baja`}
          accion={
            <Boton variante="borde" tamano="sm" iconoFinal={ArrowRight} onClick={() => ir("usuarios")}>
              Administrar
            </Boton>
          }
        >
          <ul className="space-y-2.5">
            {porRol.map((r) => (
              <li key={r.rol} className="grid grid-cols-[110px_1fr_auto] items-center gap-3 text-xs">
                <span className="font-semibold">{ROL_TXT[r.rol].replace(" del Sistema", "")}</span>
                <span className="flex h-2 overflow-hidden rounded-full bg-fondo">
                  <span className="bg-arena" style={{ width: `${(r.activos / maxRol) * 100}%` }} />
                  <span className="bg-rojo/60" style={{ width: `${(r.baja / maxRol) * 100}%` }} />
                </span>
                <span className="w-20 text-right tabular-nums text-tenue">
                  <b className="text-tinta">{r.activos}</b>
                  {r.baja > 0 && <span className="text-salmon"> +{r.baja} baja</span>}
                </span>
              </li>
            ))}
          </ul>
          <p className="etiqueta mb-2 mt-5">Altas recientes</p>
          <ul className="divide-y divide-borde/60 text-sm">
            {recientes.map((c) => (
              <li key={c.id} className="flex items-center justify-between gap-3 py-2">
                <span className="min-w-0">
                  <span className="block truncate font-semibold">{c.nombre}</span>
                  <span className="block truncate text-[11px] text-tenue">{c.correo}</span>
                </span>
                <span className={c.activo ? "badge-borde" : "badge-vino"}>{c.activo ? ROL_TXT[c.rol].replace(" del Sistema", "") : "Baja"}</span>
              </li>
            ))}
          </ul>
        </Caja>

        {/* 3 · Bitácora de eventos */}
        <Caja
          icono={ScrollText}
          titulo="Bitácora de eventos"
          sub={`Últimos ${bitacora.length} registros del servidor · operaciones críticas (RNF-22)`}
          accion={
            <Boton variante="borde" tamano="sm" iconoFinal={ArrowRight} onClick={() => ir("bitacora")}>
              Ver completa
            </Boton>
          }
        >
          {bitacora.length === 0 ? (
            <p className="text-sm text-tenue">Sin registros todavía.</p>
          ) : (
            <ol className="relative space-y-3 border-l border-borde pl-4">
              {bitacora.slice(0, 7).map((l, i) => (
                <li key={i} className="relative text-xs">
                  <span
                    className={`absolute -left-[21px] top-1 h-2.5 w-2.5 rounded-full border-2 border-panel ${
                      l.accion.includes("FALL") || l.accion.includes("EXPIR") || l.accion.includes("BAJA") ? "bg-rojo" : "bg-arena"
                    }`}
                  />
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span className="font-bold tracking-wide">{l.accion}</span>
                    <span className="text-apagado">{new Date(l.fecha).toLocaleString("es-GT", { dateStyle: "short", timeStyle: "short" })}</span>
                  </div>
                  <p className="truncate text-tenue" title={l.detalle}>
                    {l.usuario}
                    {l.detalle && ` · ${l.detalle}`}
                  </p>
                </li>
              ))}
            </ol>
          )}
        </Caja>
      </div>

      {/* 4 · Exportación de reportes */}
      <Caja icono={Download} titulo="Exportación de reportes" sub="Archivos CSV (UTF-8) listos para Excel u hojas de cálculo · RF-13 · RF-47 · RF-48">
        <div className="grid gap-3 md:grid-cols-3">
          {[
            { t: "Ventas por función", d: `${filas.length} funciones · ${filas.reduce((a, r) => a + r.vendidos, 0).toLocaleString("es-GT")} boletos`, f: () => csvVentas(filas) },
            {
              t: "Bitácora de auditoría",
              d: `${bitacora.length} registros`,
              f: () => descargarCsv("bitacora", [["fecha", "usuario", "accion", "detalle"], ...bitacora.map((l) => [l.fecha, l.usuario, l.accion, l.detalle])]),
            },
            {
              t: "Usuarios y roles",
              d: `${lista.length} cuentas`,
              f: () =>
                descargarCsv("usuarios", [
                  ["nombre", "correo", "rol", "estado", "alta"],
                  ...lista.map((c) => [c.nombre, c.correo, ROL_TXT[c.rol], c.activo ? "activo" : "baja", c.creado.slice(0, 10)]),
                ]),
            },
          ].map((x) => (
            <button key={x.t} onClick={x.f} className="group flex items-center justify-between gap-3 rounded-lg border border-borde bg-fondo/60 p-4 text-left transition-colors hover:border-arena">
              <span>
                <span className="block text-sm font-bold">{x.t}</span>
                <span className="block text-[11px] text-tenue">{x.d}</span>
              </span>
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-borde text-arena group-hover:border-arena group-hover:text-tinta">
                <Download aria-hidden className="h-4 w-4" />
              </span>
            </button>
          ))}
        </div>
      </Caja>

      {/* Operación en vivo */}
      <Caja icono={Ticket} titulo="Control de apertura y venta de taquilla" sub="Disponibilidad por sala y preventa">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-sm">
            <thead className="border-b border-borde">
              <tr>
                <th className={thCls}>Película</th>
                <th className={thCls}>Formato</th>
                <th className={thCls}>Fecha / inicio</th>
                <th className={thCls}>Estado venta</th>
                <th className={thCls}>Ocupación</th>
                <th className={`${thCls} text-right`}>Rendimiento</th>
                <th className={thCls}></th>
              </tr>
            </thead>
            <tbody>
              {proximas.map(({ f, vendidos, cap, ingresos }) => {
                const p = pelicula(f.peliculaId);
                const pct = Math.round((vendidos / cap) * 100);
                const estado = f.eventoId ? ["Preventa · cola virtual", "badge-vino"] : pct >= 100 ? ["Sold out", "badge-vino"] : ["En venta activa", "badge-borde"];
                return (
                  <tr key={f.id} className="border-b border-borde/60 last:border-0">
                    <td className="py-3 pr-3">
                      <div className="flex items-center gap-3">
                        <Poster p={p} className="aspect-[2/3] w-9 shrink-0" rounded="rounded" sizes="36px" />
                        <div>
                          <p className="font-bold">{p.titulo}</p>
                          <p className="text-[11px] text-tenue">
                            {sala(f.salaId).nombre} · {sala(f.salaId).tipo}
                          </p>
                        </div>
                      </div>
                    </td>
                    <td className="pr-3">
                      <span className="badge-borde">{f.formato}</span>
                    </td>
                    <td className="pr-3 text-xs">
                      <p className="font-semibold">{fmtFecha(f.inicio)}</p>
                      <p className="text-tenue">{fmtHora(f.inicio)} hrs</p>
                    </td>
                    <td className="pr-3">
                      <span className={estado[1]}>{estado[0]}</span>
                    </td>
                    <td className="w-44 pr-3">
                      <div className="flex justify-between text-[11px] font-bold">
                        <span>{pct}%</span>
                        <span className="text-tenue">
                          {vendidos}/{cap}
                        </span>
                      </div>
                      <div className="mt-1 h-1 overflow-hidden rounded-full bg-fondo">
                        <div className="h-full origin-left rounded-full bg-rojo" style={{ transform: `scaleX(${pct / 100})` }} />
                      </div>
                    </td>
                    <td className="pr-3 text-right text-xs">
                      <p className="font-bold text-oro">◆ {ingresos.toLocaleString("es-GT")}</p>
                      <p className="text-tenue">créditos</p>
                    </td>
                    <td className="text-right">
                      <Boton variante="borde" tamano="sm" icono={Armchair} onClick={() => setMapa(f)}>
                        Ver mapa
                      </Boton>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Caja>

      <Modal abierto={!!mapa} onCerrar={() => setMapa(null)} titulo="Mapa de la sala · solo lectura" ancho="max-w-3xl">
        {mapa && <MapaSoloLectura f={mapa} />}
      </Modal>
    </div>
  );
}
