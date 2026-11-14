"use client";

import { useCallback, useEffect } from "react";
import { ApiError, api } from "@/services/api";
import { cargarRecaptcha, ejecutarRecaptcha, guardarPase, recaptchaConfigurado, type AccionCaptcha } from "@/services/recaptcha";
import type { Resultado } from "@/types/cuenta";

/**
 * Verificación anti-bots invisible (reCAPTCHA v3) para formularios sensibles.
 *
 * Al montar precarga el script de Google para que la verificación no retrase el envío.
 * `verificar(accion)` obtiene el token y lo valida en POST /api/captcha/verificar ANTES de procesar las credenciales.
 * El servidor responde con un pase de un solo uso que POST /api/auth/registro exige para crear una cuenta nueva.
 *   - La decisión es del servidor (puntaje mínimo, acción esperada). Si no se puede verificar, se rechaza.
 *   - Sin NEXT_PUBLIC_RECAPTCHA_SITE_KEY la verificación se omite (quedan el campo trampa y el tiempo mínimo).
 */
export function useRecaptcha() {
  useEffect(() => {
    if (recaptchaConfigurado) cargarRecaptcha().catch(() => {});
  }, []);

  const verificar = useCallback(
    async (accion: AccionCaptcha): Promise<Resultado> => {
      if (!recaptchaConfigurado) return { ok: true };
      let token: string;
      try {
        token = await ejecutarRecaptcha(accion);
      } catch {
        return { ok: false, error: "No se pudo cargar la verificación anti-bots. Revisa tu conexión e inténtalo de nuevo." };
      }
      try {
        const r = await api.verificarCaptcha(token, accion);
        guardarPase(r.pase);
        return r.valido ? { ok: true } : { ok: false, error: "No pudimos confirmar que eres una persona. Inténtalo de nuevo en unos segundos." };
      } catch (e) {
        if (e instanceof ApiError && e.status === 403) {
          return { ok: false, error: "Detectamos actividad automatizada. Si eres una persona, espera un momento y vuelve a intentarlo." };
        }
        return { ok: false, error: "No se pudo verificar la solicitud con el servidor. Inténtalo de nuevo." };
      }
    },
    [],
  );

  return { verificar, activo: recaptchaConfigurado };
}
