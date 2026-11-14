"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { CircleCheck, CircleX } from "lucide-react";
import { api } from "@/services/api";
import { mensaje } from "@/store/estado";
import type { VerificacionComprobanteApi } from "@/types/api";
import { fmtFechaLarga, fmtHora } from "@/utils/formato";

// Verificación pública de un comprobante: es la página que abre su QR. El servidor comprueba la firma (HMAC) y
// responde si es auténtico, sin datos personales completos ni los códigos de las entradas.
export default function Verificar() {
  return (
    <Suspense fallback={<p className="py-24 text-center text-sm text-tenue">Verificando…</p>}>
      <Resultado />
    </Suspense>
  );
}

function Resultado() {
  const q = useSearchParams();
  const folio = q.get("folio") ?? "";
  const firma = q.get("firma") ?? "";
  const [v, setV] = useState<VerificacionComprobanteApi | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let vivo = true;
    api
      .verificarComprobante(folio, firma)
      .then((x) => vivo && setV(x))
      .catch((e) => vivo && setError(mensaje(e)));
    return () => {
      vivo = false;
    };
  }, [folio, firma]);

  if (error) return <p className="py-24 text-center text-sm text-salmon">{error}</p>;
  if (!v) return <p className="py-24 text-center text-sm text-tenue">Verificando…</p>;
  const c = v.comprobante;

  return (
    <div className={`mx-auto max-w-md rounded-xl border p-8 text-center ${v.valido ? "border-oro/50 bg-oro/10" : "border-rojo/60 bg-vino/40"}`}>
      {v.valido ? <CircleCheck aria-hidden className="mx-auto h-16 w-16 text-oro" /> : <CircleX aria-hidden className="mx-auto h-16 w-16 text-salmon" />}
      <h1 className="titulo mt-3 text-2xl">{v.valido ? "Comprobante auténtico" : "Comprobante no válido"}</h1>
      <p className={`mt-1 text-sm ${v.valido ? "text-tenue" : "text-salmon"}`}>{v.motivo}</p>
      {c && (
        <dl className="mt-6 grid grid-cols-2 gap-3 text-left text-sm">
          <div className="col-span-2">
            <dt className="etiqueta">Folio</dt>
            <dd className="font-mono font-bold">{c.folio}</dd>
          </div>
          <div className="col-span-2">
            <dt className="etiqueta">Película</dt>
            <dd className="font-semibold">{c.pelicula}</dd>
          </div>
          <div className="col-span-2">
            <dt className="etiqueta">Función</dt>
            <dd>
              {fmtFechaLarga(c.inicio)} · {fmtHora(c.inicio)} · {c.sala}
            </dd>
          </div>
          <div>
            <dt className="etiqueta">Titular</dt>
            <dd>{c.cliente}</dd>
          </div>
          <div>
            <dt className="etiqueta">Butacas</dt>
            <dd className="font-bold">{c.asientos.join(", ")}</dd>
          </div>
          <div>
            <dt className="etiqueta">Total</dt>
            <dd className="font-black text-oro">{c.total} créditos</dd>
          </div>
          <div>
            <dt className="etiqueta">Entradas</dt>
            <dd>
              {v.vigentes} vigentes · {v.utilizados} usadas
            </dd>
          </div>
        </dl>
      )}
    </div>
  );
}
