"use client";

import { ChevronLeft, ChevronRight, CircleX } from "lucide-react";
import { BotonIcono } from "@/components/ui/Boton";
import { useEffect, useState } from "react";
import { AnimatePresence, m } from "framer-motion";
import { QRCodeSVG } from "qrcode.react";
import { fmtFecha, fmtHora } from "@/utils/formato";
import { pelicula, sala } from "@/data/catalogo";
import type { Boleto } from "@/types/cuenta";
import { useApp } from "@/store/AppProvider";
import { Modal } from "@/components/ui/Modal";
import { ROTACION_S, codigoDinamico, enmascarar, ventanaActual } from "@/utils/seguridad";

const AUTOCIERRE_S = 120; // el visor se cierra solo a los 2 minutos

// Visor seguro del QR. Se carga de forma diferida (next/dynamic): qrcode.react sólo se descarga
// cuando el titular abre un boleto, no al entrar a Mis Entradas.
export default function VisorQR({ boletos, inicial, onCerrar }: { boletos: Boleto[]; inicial: number; onCerrar: () => void }) {
  const { usuario, funciones, registrar, toast } = useApp();
  const [i, setI] = useState(inicial);
  const [dir, setDir] = useState(1);
  const [ahora, setAhora] = useState(() => Date.now());
  const [desde] = useState(() => Date.now());
  const b = boletos[i];
  const f = funciones.find((x) => x.id === b.funcionId)!;
  const total = boletos.length;

  const ir = (n: number) => {
    if (n < 0 || n >= total) return;
    setDir(n > i ? 1 : -1);
    setI(n);
  };

  useEffect(() => {
    const t = setInterval(() => setAhora(Date.now()), 500);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    if (b.estado === "VÁLIDO") registrar("QR_VISUALIZADO", `${enmascarar(b.token)} · ${b.asiento}`);
  }, [registrar, b.token, b.asiento, b.estado]);

  // Flechas del teclado, cierre al cambiar de pestaña y bloqueo de capturas.
  useEffect(() => {
    const oculto = () => document.hidden && onCerrar();
    const teclas = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") ir(i + 1);
      if (e.key === "ArrowLeft") ir(i - 1);
      if (e.key === "PrintScreen") {
        onCerrar();
        toast("Las capturas no sirven para entrar: el código del QR cambia cada minuto.", "error");
      }
    };
    document.addEventListener("visibilitychange", oculto);
    window.addEventListener("keyup", teclas);
    return () => {
      document.removeEventListener("visibilitychange", oculto);
      window.removeEventListener("keyup", teclas);
    };
  });

  const restanteCierre = AUTOCIERRE_S - Math.floor((ahora - desde) / 1000);
  useEffect(() => {
    if (restanteCierre <= 0) onCerrar();
  }, [restanteCierre, onCerrar]);

  const ventana = ventanaActual(ahora);
  const codigo = codigoDinamico(b.token, ventana);
  const seg = ROTACION_S - Math.floor((ahora / 1000) % ROTACION_S);
  const valido = b.estado === "VÁLIDO";

  return (
    <Modal abierto onCerrar={onCerrar} titulo={total > 1 ? `Tus entradas · ${i + 1} de ${total}` : "Tu boleto"} z="z-[55]">
      <div className="text-center" onContextMenu={(e) => e.preventDefault()}>
        <p className="titulo">{pelicula(f.peliculaId).titulo}</p>
        <p className="text-sm text-tenue">
          {sala(f.salaId).nombre} · {fmtFecha(f.inicio)} {fmtHora(f.inicio)} · {f.formato}
        </p>

        <div className="mx-auto mt-3 inline-flex items-center gap-2 rounded-md border border-rojo/60 bg-vino px-4 py-1.5">
          <span className="etiqueta text-salmon">Butaca</span>
          <span className="text-xl font-black text-white">{b.asiento}</span>
        </div>

        <div className="relative mt-4 flex items-center justify-center gap-2 sm:gap-4">
          {total > 1 && (
            <BotonIcono icono={ChevronLeft} etiqueta="Entrada anterior" variante="borde" onClick={() => ir(i - 1)} disabled={i === 0} />
          )}
          <div className="relative overflow-hidden">
            <AnimatePresence mode="popLayout" initial={false} custom={dir}>
              <m.div
                key={b.token}
                custom={dir}
                initial={{ x: dir * 80, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: dir * -80, opacity: 0 }}
                transition={{ duration: 0.22 }}
                drag={total > 1 ? "x" : false}
                dragConstraints={{ left: 0, right: 0 }}
                dragElastic={0.4}
                onDragEnd={(_, info) => {
                  if (info.offset.x < -60) ir(i + 1);
                  else if (info.offset.x > 60) ir(i - 1);
                }}
                className="relative w-fit select-none overflow-hidden rounded-lg border-4 border-tinta/90 bg-white p-3 sm:p-4"
                draggable={false}
              >
                {valido ? (
                  <>
                    <QRCodeSVG value={JSON.stringify({ t: b.token, f: f.id, a: b.asiento, c: codigo, w: ventana })} level="M" size={200} className="h-[min(200px,55vw)] w-[min(200px,55vw)]" />
                    {/* Marca de agua con el titular: desalienta la reventa de capturas */}
                    <div className="pointer-events-none absolute inset-0 flex -rotate-[30deg] flex-col justify-center gap-6 text-[10px] font-semibold text-black/15" aria-hidden>
                      {Array.from({ length: 6 }).map((_, k) => (
                        <span key={k} className="whitespace-nowrap">
                          {usuario?.correo} · {usuario?.correo}
                        </span>
                      ))}
                    </div>
                  </>
                ) : (
                  <div className="grid h-[min(200px,55vw)] w-[min(200px,55vw)] place-items-center text-center text-black">
                    <div>
                      <CircleX aria-hidden className="mx-auto h-9 w-9" />
                      <p className="mt-1 text-sm font-black uppercase">{b.estado}</p>
                      <p className="text-xs text-black/60">Esta entrada ya no permite el acceso.</p>
                    </div>
                  </div>
                )}
              </m.div>
            </AnimatePresence>
          </div>
          {total > 1 && (
            <BotonIcono icono={ChevronRight} etiqueta="Entrada siguiente" variante="borde" onClick={() => ir(i + 1)} disabled={i === total - 1} />
          )}
        </div>

        {total > 1 && (
          <div className="mt-3 flex flex-wrap justify-center gap-1.5">
            {boletos.map((x, k) => (
              <button
                key={x.token}
                onClick={() => ir(k)}
                aria-label={`Ver entrada de la butaca ${x.asiento}`}
                className={`rounded px-2 py-1 text-[10px] font-bold transition ${k === i ? "border border-oro/60 bg-oro/15 text-oro" : "border border-borde bg-panel-2 text-tenue hover:text-tinta"} ${x.estado !== "VÁLIDO" ? "line-through" : ""}`}
              >
                {x.asiento}
              </button>
            ))}
          </div>
        )}

        {valido && (
          <>
            <div className="mx-auto mt-3 flex w-fit items-center gap-2 rounded border border-borde bg-panel-2 px-3 py-1 text-xs">
              <span className="font-black tracking-[0.25em] text-oro">{codigo}</span>
              <span className="text-tenue">se renueva en {seg}s</span>
            </div>
            <div className="mx-auto mt-2 h-1 w-48 overflow-hidden rounded-full bg-panel-2">
              <div className="h-full bg-oro transition-all duration-500" style={{ width: `${(seg / ROTACION_S) * 100}%` }} />
            </div>
          </>
        )}
        <p className="mt-2 text-[10px] tracking-wider text-apagado">N.º {enmascarar(b.token)}</p>

        <ul className="mt-4 space-y-1.5 text-left text-xs text-tenue">
          {total > 1 && <li>↔ Usa las flechas, desliza o toca una butaca para ver el QR de cada entrada.</li>}
          <li>🔄 El código cambia cada minuto: una captura de pantalla no sirve para entrar.</li>
          <li>👤 Lleva tu correo como marca de agua y cada entrada se valida una sola vez.</li>
          <li>⏳ Esta ventana se cierra sola en {Math.max(0, restanteCierre)} s o al cambiar de pestaña.</li>
        </ul>
      </div>
    </Modal>
  );
}
