"use client";

import Link from "next/link";
import { Lock, Receipt } from "lucide-react";
import { useState } from "react";
import dynamic from "next/dynamic";
import { fmtFecha, fmtHora } from "@/utils/formato";
import { pelicula, sala } from "@/data/catalogo";
import type { Boleto } from "@/types/cuenta";
import { useApp } from "@/store/AppProvider";
import { Poster } from "@/components/ui/Poster";
import { enmascarar } from "@/utils/seguridad";

const VisorQR = dynamic(() => import("./VisorQR"), { ssr: false });

const ESTADO = {
  VÁLIDO: "border-oro/50 bg-oro/10 text-oro",
  UTILIZADO: "border-borde bg-panel-2 text-tenue",
  ANULADO: "border-rojo/60 bg-vino text-salmon",
  REEMBOLSADO: "border-arena/50 bg-arena/10 text-arena",
};

// Agrupa los boletos de una misma función (misma película, horario, formato y sala).
export function agruparBoletos(boletos: Boleto[]): Boleto[][] {
  const grupos = new Map<string, Boleto[]>();
  for (const b of boletos) grupos.set(b.funcionId, [...(grupos.get(b.funcionId) ?? []), b]);
  return [...grupos.values()].map((g) => [...g].sort((a, b) => a.asiento.localeCompare(b.asiento)));
}

/** Tarjeta de una compra: un boleto, o varios de la misma función agrupados. */
export function GrupoBoletos({ boletos }: { boletos: Boleto[] }) {
  const { funciones } = useApp();
  const [visor, setVisor] = useState<number | null>(null);
  const f = funciones.find((x) => x.id === boletos[0].funcionId);
  if (!f) return null;
  const p = pelicula(f.peliculaId);
  const varios = boletos.length > 1;
  const validos = boletos.filter((b) => b.estado === "VÁLIDO").length;
  const estadoGrupo = validos ? "VÁLIDO" : boletos[0].estado;
  const primeroValido = Math.max(0, boletos.findIndex((b) => b.estado === "VÁLIDO"));

  return (
    <>
      <div className="flex overflow-hidden rounded-xl border border-borde bg-panel">
        <Poster p={p} className="hidden w-20 shrink-0 sm:block" rounded="rounded-none" sizes="80px" />
        <div className="min-w-0 flex-1 p-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`rounded border px-2 py-0.5 text-[10px] font-black uppercase tracking-[0.12em] ${ESTADO[estadoGrupo]}`}>
              {varios ? `${validos} de ${boletos.length} válidas` : boletos[0].estado}
            </span>
            {varios && <span className="badge-vino">{boletos.length} entradas</span>}
          </div>
          <h3 className="titulo mt-2 truncate">{p.titulo}</h3>
          <p className="text-xs text-tenue">
            {f.formato} · {f.idioma}
          </p>
          <dl className="mt-3 grid grid-cols-3 gap-2 text-xs">
            <div>
              <dt className="etiqueta">Sala</dt>
              <dd className="mt-0.5 font-bold">{sala(f.salaId).nombre}</dd>
            </div>
            <div>
              <dt className="etiqueta">Función</dt>
              <dd className="mt-0.5 font-bold">
                {fmtFecha(f.inicio)} {fmtHora(f.inicio)}
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="etiqueta">{varios ? "Butacas" : "Butaca"}</dt>
              <dd className="mt-0.5 truncate font-black text-oro">{boletos.map((b) => b.asiento).join(", ")}</dd>
            </div>
          </dl>
          {!varios && <p className="mt-3 text-[10px] tracking-wider text-apagado">N.º {enmascarar(boletos[0].token)}</p>}
          {/* Un comprobante por compra (folio); varias compras de la misma función se muestran juntas. */}
          <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1">
            {[...new Set(boletos.map((b) => b.folio))]
              .filter((folio) => folio?.startsWith("R-"))
              .map((folio) => (
                <Link key={folio} href={`/comprobante/${folio}`} className="inline-flex items-center gap-1 text-[11px] font-bold text-arena underline-offset-2 hover:text-tinta hover:underline">
                  <Receipt aria-hidden className="h-3 w-3" /> Comprobante {folio}
                </Link>
              ))}
          </div>
        </div>

        {/* QR protegido: borroso hasta que el titular lo abre en el visor seguro */}
        <button
          onClick={() => validos && setVisor(primeroValido)}
          disabled={!validos}
          className="group relative flex w-28 shrink-0 select-none flex-col items-center justify-center border-l border-dashed border-borde bg-white p-3 disabled:cursor-not-allowed sm:w-32"
          aria-label={validos ? (varios ? `Mostrar los ${boletos.length} códigos QR` : "Mostrar código QR") : `Boleto ${boletos[0].estado.toLowerCase()}`}
        >
          {/* En grupos, el QR se ve "apilado" para indicar varias entradas */}
          {varios && <span className="absolute inset-2 translate-x-1.5 translate-y-1.5 rounded bg-black/10" aria-hidden />}
          <div className="pointer-events-none relative blur-[6px]" aria-hidden>
            <QrDecorativo />
          </div>
          <span className="absolute inset-0 grid place-items-center bg-white/30 transition group-hover:bg-white/10">
            <span className="rounded border border-rojo/60 bg-vino px-2.5 py-1 text-center text-[9px] font-black uppercase leading-tight tracking-[0.12em] text-salmon">
              {validos && <Lock aria-hidden className="mx-auto mb-0.5 h-3 w-3" />}
              {validos ? (varios ? `Ver ${boletos.length} QR` : "Toca para ver") : boletos[0].estado}
            </span>
          </span>
        </button>
      </div>
      {visor !== null && <VisorQR boletos={boletos} inicial={visor} onCerrar={() => setVisor(null)} />}
    </>
  );
}

// Patrón con aspecto de QR para la vista borrosa de la tarjeta. No contiene datos, así que no hace falta
// la librería de QR (ni dejar el código real en la página aunque esté desenfocado).
function QrDecorativo() {
  const celdas: [number, number][] = [];
  for (let y = 0; y < 21; y++)
    for (let x = 0; x < 21; x++) {
      const esquina = (x < 7 && y < 7) || (x > 13 && y < 7) || (x < 7 && y > 13);
      if (esquina ? x % 6 === 0 || y % 6 === 0 || (x % 7 > 1 && x % 7 < 5 && y % 7 > 1 && y % 7 < 5) || x === 20 || y === 20 : (x * 7 + y * 13 + x * y) % 3 === 0)
        celdas.push([x, y]);
    }
  return (
    <svg viewBox="0 0 21 21" width={84} height={84} shapeRendering="crispEdges">
      {celdas.map(([x, y]) => (
        <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill="#000" />
      ))}
    </svg>
  );
}
