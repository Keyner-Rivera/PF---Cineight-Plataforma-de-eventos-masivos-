"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
import { AnimatePresence, m } from "framer-motion";
import { ChevronDown, KeyRound, LayoutDashboard, LogOut, MonitorSmartphone, ShieldCheck, Ticket } from "lucide-react";
import { ROL_TXT } from "@/data/catalogo";
import { useApp } from "@/store/AppProvider";
import { useCerrarSesion } from "@/hooks/useCerrarSesion";

const iniciales = (nombre: string) =>
  nombre
    .split(" ")
    .map((x) => x[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();

/**
 * Usuario en la esquina superior derecha: avatar + nombre/correo. Al hacer clic despliega un menú
 * con los datos de la sesión, cambiar la contraseña y cerrar la sesión (en este dispositivo o en todos; el servidor la
 * revoca). Se cierra con Esc, clic fuera o al elegir una opción.
 */
export function MenuUsuario() {
  const { usuario, abrirCambioClave, cerrarSesionesTodas, toast } = useApp();
  const cerrarSesion = useCerrarSesion();
  const router = useRouter();
  const [abierto, setAbierto] = useState(false);
  const caja = useRef<HTMLDivElement>(null);
  const idMenu = useId();

  useEffect(() => {
    if (!abierto) return;
    const fuera = (e: PointerEvent) => !caja.current?.contains(e.target as Node) && setAbierto(false);
    const tecla = (e: KeyboardEvent) => e.key === "Escape" && setAbierto(false);
    document.addEventListener("pointerdown", fuera);
    document.addEventListener("keydown", tecla);
    return () => {
      document.removeEventListener("pointerdown", fuera);
      document.removeEventListener("keydown", tecla);
    };
  }, [abierto]);

  if (!usuario) return null;
  const staff = usuario.rol !== "cliente";

  return (
    <div ref={caja} className="relative">
      <button
        type="button"
        onClick={() => setAbierto((x) => !x)}
        aria-haspopup="menu"
        aria-expanded={abierto}
        aria-controls={idMenu}
        className={`flex items-center gap-2.5 rounded-full border py-1 pl-1 pr-2 transition-colors sm:pr-3 ${abierto ? "border-arena bg-panel-2" : "border-borde bg-panel hover:border-arena"}`}
      >
        <span className="grid h-8 w-8 place-items-center rounded-full border border-arena/60 bg-panel-2 text-[11px] font-black text-arena">{iniciales(usuario.nombre)}</span>
        <span className="hidden max-w-[180px] text-left leading-tight md:block">
          <span className="block truncate text-xs font-bold">{staff ? usuario.correo : usuario.nombre}</span>
          <span className="block truncate text-[10px] uppercase tracking-wider text-tenue">{ROL_TXT[usuario.rol]}</span>
        </span>
        <ChevronDown aria-hidden className={`h-4 w-4 text-tenue transition-transform ${abierto ? "rotate-180" : ""}`} />
      </button>

      <AnimatePresence>
        {abierto && (
          <m.div
            id={idMenu}
            role="menu"
            aria-label="Menú de usuario"
            initial={{ opacity: 0, y: -6, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -6, scale: 0.97 }}
            transition={{ duration: 0.14 }}
            className="absolute right-0 top-[calc(100%+8px)] z-50 w-72 origin-top-right overflow-hidden rounded-xl border border-borde bg-panel shadow-2xl shadow-black/60"
          >
            <div className="border-b border-borde p-4">
              <p className="truncate text-sm font-bold">{usuario.nombre}</p>
              <p className="truncate text-xs text-tenue">{usuario.correo}</p>
              <div className="mt-2 flex flex-wrap gap-1.5">
                <span className={staff ? "badge-vino" : "badge-borde"}>{ROL_TXT[usuario.rol]}</span>
                {usuario.rol === "auditor" && (
                  <span className="badge-borde inline-flex items-center gap-1">
                    <ShieldCheck aria-hidden className="h-3 w-3" /> Solo lectura
                  </span>
                )}
              </div>
            </div>
            <div className="p-1.5">
              {staff ? (
                <ItemEnlace href="/panel" icono={LayoutDashboard} onClick={() => setAbierto(false)}>
                  Panel de operaciones
                </ItemEnlace>
              ) : (
                <ItemEnlace href="/mis-entradas" icono={Ticket} onClick={() => setAbierto(false)}>
                  Mis Entradas
                </ItemEnlace>
              )}
              <ItemBoton
                icono={KeyRound}
                onClick={() => {
                  setAbierto(false);
                  abrirCambioClave();
                }}
              >
                Cambiar contraseña
              </ItemBoton>
              <ItemBoton
                icono={MonitorSmartphone}
                onClick={async () => {
                  setAbierto(false);
                  const r = await cerrarSesionesTodas();
                  if (!r.ok) return toast(r.error!, "error");
                  toast("Cerramos tu sesión en todos los dispositivos.", "ok");
                  router.replace("/");
                }}
              >
                Cerrar sesión en todos los dispositivos
              </ItemBoton>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setAbierto(false);
                  cerrarSesion();
                }}
                className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm font-semibold text-salmon transition-colors hover:bg-vino/60 hover:text-white"
              >
                <LogOut aria-hidden className="h-4 w-4" />
                Cerrar sesión
              </button>
            </div>
          </m.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function ItemBoton({ icono: Icono, onClick, children }: { icono: typeof Ticket; onClick: () => void; children: React.ReactNode }) {
  return (
    <button type="button" role="menuitem" onClick={onClick} className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm font-semibold text-tinta transition-colors hover:bg-panel-2">
      <Icono aria-hidden className="h-4 w-4 text-arena" />
      {children}
    </button>
  );
}

function ItemEnlace({ href, icono: Icono, onClick, children }: { href: string; icono: typeof Ticket; onClick: () => void; children: React.ReactNode }) {
  return (
    <Link href={href} role="menuitem" onClick={onClick} className="flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-semibold text-tinta transition-colors hover:bg-panel-2">
      <Icono aria-hidden className="h-4 w-4 text-arena" />
      {children}
    </Link>
  );
}
