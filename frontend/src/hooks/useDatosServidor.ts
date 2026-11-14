"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/services/api";
import { useApp } from "@/store/AppProvider";
import type { BitacoraApi, UsuarioApi } from "@/types/api";

/** Cuentas registradas (GET /api/usuarios, sólo administrador). `recargar` vuelve a pedirlas tras un cambio. */
export function useUsuarios() {
  const { usuario, modoApi } = useApp();
  const [usuarios, setUsuarios] = useState<UsuarioApi[] | null>(null);
  const [version, setVersion] = useState(0);
  const admin = usuario?.rol === "admin";
  useEffect(() => {
    if (!admin || modoApi !== "conectado") return;
    let vivo = true;
    api
      .usuarios()
      .then((u) => vivo && setUsuarios(u))
      .catch(() => vivo && setUsuarios([]));
    return () => {
      vivo = false;
    };
  }, [admin, modoApi, version]);
  const recargar = useCallback(() => setVersion((v) => v + 1), []);
  return { usuarios, recargar };
}

/** Bitácora de auditoría del servidor (GET /api/bitacora, administrador o auditor), actualizada cada 15 s. */
export function useBitacora() {
  const { usuario, modoApi } = useApp();
  const [bitacora, setBitacora] = useState<BitacoraApi[] | null>(null);
  const puede = usuario?.rol === "admin" || usuario?.rol === "auditor";
  useEffect(() => {
    if (!puede || modoApi !== "conectado") return;
    let vivo = true;
    const traer = () =>
      api
        .bitacora()
        .then((b) => vivo && setBitacora(b))
        .catch(() => {});
    traer();
    const t = setInterval(traer, 15000);
    return () => {
      vivo = false;
      clearInterval(t);
    };
  }, [puede, modoApi]);
  return bitacora;
}
