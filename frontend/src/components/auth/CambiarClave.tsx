"use client";

import { useState } from "react";
import { Check, KeyRound, LogOut, X } from "lucide-react";
import { useApp } from "@/store/AppProvider";
import { Modal } from "@/components/ui/Modal";
import { Boton } from "@/components/ui/Boton";
import { requisitosClave, validarClaveNueva } from "@/utils/validacion";

/**
 * Cambio de contraseña (POST /api/auth/clave). Con `obligatoria` (contraseña temporal del personal o asignada por el
 * administrador) no se puede cerrar: hay que cambiarla o cerrar sesión. Al cambiarla, el servidor cierra las sesiones
 * de los demás dispositivos.
 */
export function CambiarClave({ obligatoria, onCerrar }: { obligatoria: boolean; onCerrar: () => void }) {
  const { usuario, cambiarClave, logout, toast } = useApp();
  const [actual, setActual] = useState("");
  const [nueva, setNueva] = useState("");
  const [confirmacion, setConfirmacion] = useState("");
  const [error, setError] = useState("");
  const [enviando, setEnviando] = useState(false);
  const correo = usuario?.correo ?? "";

  const enviar = async (e: React.FormEvent) => {
    e.preventDefault();
    if (enviando) return;
    const invalida = validarClaveNueva(nueva, correo);
    if (!actual) return setError("Escribe tu contraseña actual.");
    if (invalida) return setError(invalida);
    if (nueva !== confirmacion) return setError("Las contraseñas nuevas no coinciden.");
    if (nueva === actual) return setError("La contraseña nueva debe ser distinta de la actual.");
    setEnviando(true);
    const r = await cambiarClave(actual, nueva);
    setEnviando(false);
    if (!r.ok) return setError(r.error!);
    toast("Contraseña actualizada. Cerramos tu sesión en los demás dispositivos.", "ok");
    onCerrar();
  };

  return (
    <Modal abierto onCerrar={onCerrar} cerrable={!obligatoria} titulo={obligatoria ? "Crea tu contraseña" : "Cambiar contraseña"} z="z-[60]">
      <form onSubmit={enviar} className="space-y-3" noValidate>
        {obligatoria && (
          <p className="rounded-md border border-oro/40 bg-oro/10 p-3 text-sm text-oro">
            Tu cuenta tiene una contraseña temporal. Por seguridad, crea una propia antes de continuar.
          </p>
        )}
        <label className="etiqueta block">
          Contraseña actual
          <input type="password" autoComplete="current-password" value={actual} onChange={(e) => (setActual(e.target.value), setError(""))} className="campo" />
        </label>
        <label className="etiqueta block">
          Contraseña nueva
          <input type="password" autoComplete="new-password" value={nueva} onChange={(e) => (setNueva(e.target.value), setError(""))} maxLength={72} className="campo" />
        </label>
        <ul className="grid gap-1 text-[11px]">
          {requisitosClave(nueva, correo).map((r) => (
            <li key={r.texto} className={`flex items-center gap-1.5 ${r.ok ? "text-oro" : "text-tenue"}`}>
              {r.ok ? <Check aria-hidden className="h-3 w-3" /> : <X aria-hidden className="h-3 w-3" />} {r.texto}
            </li>
          ))}
        </ul>
        <label className="etiqueta block">
          Repite la contraseña nueva
          <input type="password" autoComplete="new-password" value={confirmacion} onChange={(e) => (setConfirmacion(e.target.value), setError(""))} maxLength={72} className="campo" />
        </label>
        {error && <p className="rounded-md border border-rojo/60 bg-vino/50 p-2.5 text-sm text-salmon">{error}</p>}
        <Boton type="submit" icono={KeyRound} tamano="lg" cargando={enviando} className="w-full">
          Guardar contraseña
        </Boton>
        {obligatoria && (
          <button type="button" onClick={logout} className="btn-fantasma mx-auto flex text-xs">
            <LogOut aria-hidden className="h-3.5 w-3.5" /> Cerrar sesión
          </button>
        )}
      </form>
    </Modal>
  );
}
