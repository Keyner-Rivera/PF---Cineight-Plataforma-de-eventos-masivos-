"use client";

import Link from "next/link";
import { useState } from "react";
import { m } from "framer-motion";
import { Gem, MonitorPlay, Users } from "lucide-react";
import { sala } from "@/data/catalogo";
import { claveDia, fmtDiaRelativo, fmtFechaLarga, fmtHora } from "@/utils/formato";
import type { Funcion } from "@/types/catalogo";
import { useApp } from "@/store/AppProvider";

/**
 * Horarios agrupados por día en pestañas. Dentro de cada día, una fila por sala/formato/idioma con sus
 * horarios dispuestos en horizontal: aprovecha el ancho de la pantalla y muestra muchas funciones de un vistazo.
 */
export function HorariosPorDia({ funciones }: { funciones: Funcion[] }) {
  const { ocupacion } = useApp();

  const dias = new Map<string, Funcion[]>();
  for (const f of funciones) dias.set(claveDia(f.inicio), [...(dias.get(claveDia(f.inicio)) ?? []), f]);
  const claves = [...dias.keys()];
  const [elegido, setElegido] = useState<string | null>(null);
  const dia = elegido && dias.has(elegido) ? elegido : claves[0];
  const delDia = dias.get(dia) ?? [];

  // Filas: misma sala, formato e idioma.
  const filas = new Map<string, Funcion[]>();
  for (const f of delDia) {
    const k = `${f.salaId}|${f.formato}|${f.idioma}`;
    filas.set(k, [...(filas.get(k) ?? []), f]);
  }

  return (
    <div>
      {/* Pestañas de días */}
      <div role="tablist" aria-label="Días con funciones" className="sin-scrollbar -mx-4 flex gap-2 overflow-x-auto px-4 pb-1 sm:mx-0 sm:px-0">
        {claves.map((k) => {
          const lista = dias.get(k)!;
          const activo = k === dia;
          const fecha = new Date(lista[0].inicio);
          return (
            <m.button
              key={k}
              type="button"
              role="tab"
              aria-selected={activo}
              onClick={() => setElegido(k)}
              whileTap={{ scale: 0.96 }}
              className={`relative min-w-[92px] shrink-0 rounded-lg border px-4 py-2.5 text-left transition-colors ${
                activo ? "border-rojo/70 bg-vino text-salmon" : "border-borde bg-panel text-tinta hover:border-arena"
              }`}
            >
              <span className="block text-[10px] font-bold uppercase tracking-[0.16em] opacity-80">{fmtDiaRelativo(lista[0].inicio)}</span>
              <span className="mt-0.5 block text-lg font-black leading-none">
                {fecha.toLocaleDateString("es-GT", { day: "2-digit" })} <span className="text-xs font-bold uppercase">{fecha.toLocaleDateString("es-GT", { month: "short" })}</span>
              </span>
              <span className={`mt-1 block text-[10px] font-semibold ${activo ? "text-salmon/80" : "text-tenue"}`}>
                {lista.length} {lista.length === 1 ? "función" : "funciones"}
              </span>
            </m.button>
          );
        })}
      </div>

      {/* key={dia}: al cambiar de pestaña se monta un panel nuevo con su animación de entrada */}
      <m.div key={dia} role="tabpanel" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }} className="mt-5">
          <p className="etiqueta mb-3 text-tinta first-letter:uppercase">{delDia[0] && fmtFechaLarga(delDia[0].inicio)}</p>
          <div className="divide-y divide-borde overflow-hidden rounded-xl border border-borde bg-panel">
            {[...filas.values()].map((lista) => {
              const s = sala(lista[0].salaId);
              return (
                <div key={lista[0].id} className="grid gap-3 p-4 md:grid-cols-[220px_1fr] md:items-center">
                  <div className="flex items-start gap-3">
                    <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-borde bg-panel-2 text-arena">
                      <MonitorPlay aria-hidden className="h-4 w-4" />
                    </span>
                    <div>
                      <p className="text-sm font-bold">
                        {s.nombre} · {s.tipo}
                      </p>
                      <p className="text-xs text-tenue">
                        {lista[0].formato} · {lista[0].idioma}
                      </p>
                    </div>
                  </div>
                  <div className="grid grid-cols-[repeat(auto-fill,minmax(118px,1fr))] gap-2">
                    {lista.map((f) => {
                      const o = ocupacion(f.id);
                      const libres = o.libres(s.aforo);
                      return (
                        <Link
                          key={f.id}
                          href={f.eventoId ? `/cola/${f.eventoId}` : `/funcion/${f.id}`}
                          className={`group rounded-lg border px-3 py-2.5 transition-[transform,border-color] hover:-translate-y-0.5 ${
                            f.eventoId ? "border-rojo/60 bg-vino/40 hover:border-salmon" : "border-borde bg-fondo hover:border-arena"
                          }`}
                        >
                          <span className="block text-xl font-black tabular-nums group-hover:text-oro">{fmtHora(f.inicio)}</span>
                          <span className="mt-1 flex items-center justify-between text-[11px] font-bold">
                            <span className="flex items-center gap-1 text-oro">
                              <Gem aria-hidden className="h-3 w-3" /> {f.precio}
                            </span>
                            <span className={libres < 15 ? "text-salmon" : "text-tenue"}>{libres} libres</span>
                          </span>
                          {f.eventoId && (
                            <span className="mt-1 flex items-center gap-1 text-[9px] font-black uppercase tracking-[0.12em] text-salmon">
                              <Users aria-hidden className="h-3 w-3" /> Cola virtual
                            </span>
                          )}
                        </Link>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>
      </m.div>
    </div>
  );
}
