"use client";

import { m } from "framer-motion";
import {
  Armchair,
  BarChart3,
  Clapperboard,
  CalendarClock,
  HeartHandshake,
  LayoutDashboard,
  ScanLine,
  ScrollText,
  SlidersHorizontal,
  Users,
  Warehouse,
  type LucideIcon,
} from "lucide-react";
import type { Rol } from "@/types/catalogo";

export type IdSeccion = "general" | "clientes" | "peliculas" | "usuarios" | "parametros" | "reportes" | "bitacora" | "programar" | "inventario" | "butacas" | "validar";
type Seccion = { id: IdSeccion; txt: string; icono: LucideIcon };

/** Secciones visibles según el rol (RF-44). */
export const SECCIONES: Record<Exclude<Rol, "cliente">, Seccion[]> = {
  agente: [
    { id: "peliculas", txt: "Películas", icono: Clapperboard },
    { id: "programar", txt: "Programación", icono: CalendarClock },
    { id: "inventario", txt: "Aforos & Salas", icono: Warehouse },
    { id: "butacas", txt: "Butacas", icono: Armchair },
    { id: "validar", txt: "Validar acceso", icono: ScanLine },
  ],
  admin: [
    { id: "general", txt: "Panel general", icono: LayoutDashboard },
    { id: "clientes", txt: "Clientes", icono: HeartHandshake },
    { id: "peliculas", txt: "Películas", icono: Clapperboard },
    { id: "programar", txt: "Programación", icono: CalendarClock },
    { id: "butacas", txt: "Butacas", icono: Armchair },
    { id: "usuarios", txt: "Usuarios & roles", icono: Users },
    { id: "parametros", txt: "Parámetros", icono: SlidersHorizontal },
    { id: "reportes", txt: "Reportes", icono: BarChart3 },
    { id: "bitacora", txt: "Bitácora", icono: ScrollText },
  ],
  auditor: [
    { id: "reportes", txt: "Reportes", icono: BarChart3 },
    { id: "bitacora", txt: "Bitácora", icono: ScrollText },
  ],
};

const TITULOS: Record<IdSeccion, [string, string]> = {
  general: ["Dashboard de operaciones y taquilla en tiempo real", "Ventas, ocupación y actividad del día"],
  clientes: ["Estadísticas de clientes", "Historial de compras y preferencias: películas, géneros, directores y actores"],
  peliculas: ["Catálogo de películas", "Publicar películas con póster, ficha técnica y funciones (RF-05, RF-67)"],
  usuarios: ["Gestión de usuarios y roles", "Crear, editar y dar de baja cuentas · requiere reconfirmar identidad"],
  parametros: ["Parámetros globales", "Configurables sin modificar el código fuente (RF-57)"],
  reportes: ["Reportes de ventas y ocupación", "RF-13 · RF-47 · RF-48"],
  bitacora: ["Bitácora de auditoría", "Registro cronológico de operaciones críticas (RNF-22)"],
  programar: ["Programación de funciones", "Sin traslapes: inicio + duración + 20 min de limpieza (RF-31)"],
  inventario: ["Aforos & salas", "Inventario por función (RF-66)"],
  butacas: ["Butacas fuera de servicio", "Retira de la venta las butacas dañadas o en mantenimiento"],
  validar: ["Validación de acceso a sala", "Lector QR en modo teclado (RF-09, RF-27)"],
};

type Props = {
  secciones: Seccion[];
  activa: IdSeccion;
  onCambiar: (id: IdSeccion) => void;
  children: React.ReactNode;
};

/**
 * Estructura del panel de operaciones.
 * Escritorio (lg+): el marco mide exactamente el alto de la ventana menos el encabezado (sin flex-grow, que lo haría
 * crecer con el contenido). La barra lateral nunca se mueve y el área de contenido es el único elemento con scroll.
 * Móvil: la navegación pasa a una fila de pestañas fija bajo el encabezado y la página se desplaza normalmente.
 */
export function MarcoPanel({ secciones, activa, onCambiar, children }: Props) {
  const [titulo, sub] = TITULOS[activa];
  return (
    <div className="flex flex-1 flex-col lg:h-[calc(100dvh-var(--alto-header))] lg:flex-none lg:flex-row lg:overflow-hidden">
      {/* Barra lateral */}
      <aside className="sticky top-[var(--alto-header)] z-30 shrink-0 border-b border-borde bg-fondo/95 backdrop-blur lg:static lg:flex lg:w-64 lg:flex-col lg:overflow-y-auto lg:border-b-0 lg:border-r lg:bg-panel/60 lg:p-5">
        <div className="hidden lg:block">
          <p className="titulo text-base">Cineight</p>
          <p className="etiqueta text-oro">Operaciones</p>
          <p className="etiqueta mt-6">Gestión & auditoría</p>
        </div>
        <nav aria-label="Secciones del panel" className="sin-scrollbar flex gap-1 overflow-x-auto px-3 py-2 lg:mt-3 lg:flex-col lg:overflow-visible lg:p-0">
          {secciones.map(({ id, txt, icono: Icono }) => {
            const actual = activa === id;
            return (
              <m.button
                key={id}
                type="button"
                onClick={() => onCambiar(id)}
                whileTap={{ scale: 0.97 }}
                aria-current={actual ? "page" : undefined}
                className={`relative flex items-center gap-3 whitespace-nowrap rounded-md px-3 py-2.5 text-left text-sm font-semibold transition-colors ${
                  actual ? "bg-panel-2 text-tinta" : "text-tenue hover:bg-panel-2/60 hover:text-tinta"
                }`}
              >
                {actual && <span aria-hidden className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-oro lg:inset-x-auto lg:inset-y-2 lg:left-0 lg:h-auto lg:w-0.5" />}
                <Icono aria-hidden className={`h-4 w-4 ${actual ? "text-oro" : "text-arena"}`} />
                {txt}
              </m.button>
            );
          })}
        </nav>
      </aside>

      {/* Contenido: único elemento con scroll en escritorio */}
      <section className="min-w-0 flex-1 p-4 sm:p-6 lg:h-full lg:min-h-0 lg:overflow-y-auto lg:overscroll-contain lg:p-8">
        <div className="mx-auto max-w-[1400px]">
          <div className="mb-6 flex flex-wrap items-start justify-between gap-4 border-b border-borde pb-5">
            <div>
              <p className="etiqueta flex items-center gap-2">
                <span className="h-1.5 w-1.5 rounded-full bg-oro" /> {sub}
              </p>
              <h1 className="titulo mt-2 text-2xl leading-tight sm:text-3xl">{titulo}</h1>
            </div>
          </div>
          {children}
        </div>
      </section>
    </div>
  );
}
