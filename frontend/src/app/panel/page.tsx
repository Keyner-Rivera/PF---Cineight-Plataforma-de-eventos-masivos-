"use client";

import { useState } from "react";
import { LogIn, ShieldAlert } from "lucide-react";
import { useApp } from "@/store/AppProvider";
import { Boton } from "@/components/ui/Boton";
import { MarcoPanel, SECCIONES, type IdSeccion } from "@/components/panel/MarcoPanel";
import { General } from "@/components/panel/General";
import { Clientes } from "@/components/panel/Clientes";
import { Peliculas } from "@/components/panel/Peliculas";
import { Usuarios } from "@/components/panel/Usuarios";
import { Parametros } from "@/components/panel/Parametros";
import { Reportes } from "@/components/panel/Reportes";
import { Bitacora } from "@/components/panel/Bitacora";
import { Programar } from "@/components/panel/Programar";
import { Inventario } from "@/components/panel/Inventario";
import { Validar } from "@/components/panel/Validar";
import { Butacas } from "@/components/panel/Butacas";

// Panel de operaciones del personal (agente, administrador, auditor).
// Los datos de la sesión y "Cerrar sesión" están en el menú de usuario del encabezado.
export default function Panel() {
  const { usuario, listo, abrirLogin } = useApp();
  const [sec, setSec] = useState<IdSeccion | "">("");

  if (!listo) return null;
  if (!usuario || usuario.rol === "cliente")
    return (
      <div className="px-4 py-16">
        <div className="tarjeta mx-auto max-w-md p-8 text-center">
          <ShieldAlert aria-hidden className="mx-auto h-10 w-10 text-salmon" />
          <p className="titulo mt-3 text-lg">Acceso restringido</p>
          <p className="mt-2 text-sm text-tenue">El panel de operaciones es exclusivo para el personal (RNF-23).</p>
          <Boton icono={LogIn} onClick={() => abrirLogin()} className="mt-5">
            Iniciar sesión con un rol interno
          </Boton>
        </div>
      </div>
    );

  const secciones = SECCIONES[usuario.rol];
  const activa = secciones.find((s) => s.id === sec)?.id ?? secciones[0].id;

  return (
    <MarcoPanel secciones={secciones} activa={activa} onCambiar={setSec}>
      {activa === "general" && <General ir={setSec} />}
      {activa === "clientes" && <Clientes />}
      {activa === "peliculas" && <Peliculas />}
      {activa === "usuarios" && <Usuarios />}
      {activa === "parametros" && <Parametros />}
      {activa === "reportes" && <Reportes />}
      {activa === "bitacora" && <Bitacora />}
      {activa === "programar" && <Programar />}
      {activa === "inventario" && <Inventario />}
      {activa === "butacas" && <Butacas />}
      {activa === "validar" && <Validar />}
    </MarcoPanel>
  );
}
