"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { QRCodeSVG } from "qrcode.react";
import { LogIn, Printer, ShieldCheck, Ticket } from "lucide-react";
import { useApp } from "@/store/AppProvider";
import { api } from "@/services/api";
import { mensaje } from "@/store/estado";
import type { ComprobanteApi } from "@/types/api";
import { fmtFechaLarga, fmtHora } from "@/utils/formato";
import { Boton, BotonEnlace } from "@/components/ui/Boton";

/**
 * Comprobante de una compra (folio = id de la reserva pagada). Los datos y el total vienen del servidor, que además lo
 * firma (HMAC): el QR lleva a /verificar, donde cualquiera puede confirmar que es auténtico y no fue alterado.
 * Se imprime o guarda como PDF con el diálogo de impresión del navegador.
 */
export default function Comprobante() {
  const { folio } = useParams<{ folio: string }>();
  const { usuario, listo, abrirLogin } = useApp();
  const [c, setC] = useState<ComprobanteApi | null>(null);
  const [error, setError] = useState("");
  const idUsuario = usuario?.id;

  useEffect(() => {
    if (!idUsuario) return;
    let vivo = true;
    api
      .comprobante(folio)
      .then((x) => vivo && setC(x))
      .catch((e) => vivo && setError(mensaje(e)));
    return () => {
      vivo = false;
    };
  }, [folio, idUsuario]);

  if (listo && !usuario)
    return (
      <div className="tarjeta mx-auto max-w-md p-8 text-center">
        <p className="titulo text-lg">Comprobante de compra</p>
        <p className="mt-2 text-sm text-tenue">Inicia sesión con la cuenta que hizo la compra para ver su comprobante.</p>
        <Boton icono={LogIn} onClick={() => abrirLogin()} className="mt-5">
          Iniciar sesión
        </Boton>
      </div>
    );
  if (error)
    return (
      <div className="tarjeta mx-auto max-w-md p-8 text-center">
        <p className="titulo text-lg">Comprobante no disponible</p>
        <p className="mt-2 text-sm text-salmon">{error}</p>
        <BotonEnlace href="/mis-entradas" icono={Ticket} className="mt-5">
          Mis Entradas
        </BotonEnlace>
      </div>
    );
  if (!c) return <p className="py-24 text-center text-sm text-tenue">Cargando el comprobante…</p>;

  const urlVerificacion = `${window.location.origin}/verificar?folio=${encodeURIComponent(c.folio)}&firma=${encodeURIComponent(c.firma ?? "")}`;
  const emitido = new Date(c.fecha);

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <div className="flex flex-wrap gap-2 print:hidden">
        <Boton icono={Printer} onClick={() => window.print()}>
          Imprimir o guardar PDF
        </Boton>
        <BotonEnlace href="/mis-entradas" icono={Ticket} variante="borde">
          Mis Entradas
        </BotonEnlace>
      </div>

      {/* Hoja del comprobante: en papel se imprime sola, en blanco y negro legible */}
      <article className="comprobante-imprimible overflow-hidden rounded-xl border border-borde bg-panel">
        <header className="flex flex-wrap items-start justify-between gap-4 border-b border-dashed border-borde p-6">
          <div>
            <p className="titulo text-2xl">Cineight</p>
            <p className="etiqueta text-oro">Comprobante de compra</p>
          </div>
          <dl className="text-right text-xs">
            <dt className="etiqueta">Folio</dt>
            <dd className="font-mono text-base font-black">{c.folio}</dd>
            <dt className="etiqueta mt-2">Emitido</dt>
            <dd>
              {emitido.toLocaleDateString("es-GT", { day: "2-digit", month: "long", year: "numeric" })} · {fmtHora(emitido)}
            </dd>
          </dl>
        </header>

        <div className="grid gap-6 p-6 sm:grid-cols-[1fr_auto]">
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
            <Dato t="Cliente" v={c.cliente} />
            <Dato t="Correo" v={c.correo ?? ""} />
            <Dato t="Película" v={c.pelicula} ancho />
            <Dato t="Función" v={`${fmtFechaLarga(c.inicio)} · ${fmtHora(c.inicio)}`} ancho />
            <Dato t="Sala" v={c.sala} />
            <Dato t="Formato" v={`${c.formato} · ${c.idioma}`} />
            <Dato t={c.asientos.length > 1 ? "Butacas" : "Butaca"} v={c.asientos.join(", ")} ancho />
          </dl>
          <figure className="mx-auto w-fit text-center">
            <div className="rounded-lg bg-white p-3">
              <QRCodeSVG value={urlVerificacion} level="M" size={132} />
            </div>
            <figcaption className="mt-2 max-w-[160px] text-[10px] leading-snug text-tenue">Escanéalo para verificar que el comprobante es auténtico</figcaption>
          </figure>
        </div>

        <table className="mx-6 mb-6 w-[calc(100%-3rem)] text-sm">
          <thead className="border-b border-borde text-left">
            <tr>
              <th className="etiqueta pb-2 font-bold">Concepto</th>
              <th className="etiqueta pb-2 text-right font-bold">Cant.</th>
              <th className="etiqueta pb-2 text-right font-bold">Precio</th>
              <th className="etiqueta pb-2 text-right font-bold">Importe</th>
            </tr>
          </thead>
          <tbody>
            <tr className="border-b border-borde/60">
              <td className="py-2">Entrada · {c.pelicula}</td>
              <td className="py-2 text-right tabular-nums">{c.asientos.length}</td>
              <td className="py-2 text-right tabular-nums">{c.precio_unitario} cr.</td>
              <td className="py-2 text-right tabular-nums">{c.total} cr.</td>
            </tr>
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={3} className="pt-3 text-right font-bold">
                Total pagado
              </td>
              <td className="pt-3 text-right text-lg font-black tabular-nums text-oro">{c.total} créditos</td>
            </tr>
          </tfoot>
        </table>

        <footer className="flex items-start gap-2 border-t border-dashed border-borde bg-fondo/40 p-4 text-[11px] text-tenue">
          <ShieldCheck aria-hidden className="h-4 w-4 shrink-0 text-oro" />
          <p>
            Importe calculado por el servidor con la tarifa vigente de la función. Firma de verificación{" "}
            <span className="font-mono text-tinta">{c.firma}</span>. Para entrar a la sala presenta el QR de cada entrada desde Mis Entradas (cambia cada
            minuto); este comprobante acredita la compra pero no da acceso.
          </p>
        </footer>
      </article>
    </div>
  );
}

function Dato({ t, v, ancho }: { t: string; v: string; ancho?: boolean }) {
  return (
    <div className={ancho ? "col-span-2" : ""}>
      <dt className="etiqueta">{t}</dt>
      <dd className="mt-0.5 font-semibold">{v}</dd>
    </div>
  );
}
