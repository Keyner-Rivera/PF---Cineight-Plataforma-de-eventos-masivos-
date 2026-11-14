"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Check, Circle, Eye, EyeOff, LogIn, UserPlus } from "lucide-react";
import { PARAMETROS } from "@/data/parametros";
import type { Rol } from "@/types/catalogo";
import { useApp } from "@/store/AppProvider";
import { useRecaptcha } from "@/hooks/useRecaptcha";
import { Modal } from "@/components/ui/Modal";
import { Boton } from "@/components/ui/Boton";
import { AvisoRecaptcha } from "./AvisoRecaptcha";
import {
  CLAVE_MAX,
  CORREO_MAX,
  NOMBRE_MAX,
  normalizarCorreo,
  normalizarNombre,
  requisitosClave,
  validarClaveLogin,
  validarClaveNueva,
  validarCorreo,
  validarNombre,
} from "@/utils/validacion";

type CampoForm = "nombre" | "correo" | "clave";

const TIEMPO_MINIMO_MS = 1500; // un humano no completa el formulario en menos de 1.5 s
const msDesde = (t: number) => Date.now() - t;
const ahoraMs = () => Date.now();

/** Inicio de sesión / registro en ventana emergente: al terminar, el usuario sigue en la misma página. */
export function LoginModal() {
  const { loginAbierto, cerrarLogin, modoLogin } = useApp();
  return (
    <Modal abierto={loginAbierto} onCerrar={cerrarLogin} ancho="max-w-md" z="z-[58]" etiqueta={modoLogin === "registro" ? "Crear cuenta" : "Iniciar sesión"}>
      {loginAbierto && <Contenido key={modoLogin} modoInicial={modoLogin} />}
    </Modal>
  );
}

/**
 * Anti-bots en tres capas, ninguna visible para la persona (RF-53):
 *   1. Campo trampa oculto que sólo rellenan los bots.
 *   2. Tiempo mínimo entre abrir el formulario y enviarlo.
 *   3. reCAPTCHA v3: puntaje calculado en segundo plano y verificado por el backend en Go
 *      ANTES de procesar las credenciales.
 */
function Contenido({ modoInicial }: { modoInicial: "login" | "registro" }) {
  const router = useRouter();
  const { toast, iniciarSesion, registrarCliente, cerrarLogin } = useApp();
  const { verificar } = useRecaptcha();
  const [modo, setModo] = useState(modoInicial);
  const [nombre, setNombre] = useState("");
  const [correo, setCorreo] = useState("");
  const [clave, setClave] = useState("");
  const [trampa, setTrampa] = useState(""); // honeypot: invisible para personas
  const [error, setError] = useState("");
  const [verificando, setVerificando] = useState(false);
  const [verClave, setVerClave] = useState(false);
  // Un campo muestra su error después de salir de él (o al intentar enviar), no mientras se escribe por primera vez.
  const [tocados, setTocados] = useState<Partial<Record<CampoForm, boolean>>>({});

  // Reglas: en el registro se valida todo (nombre completo, correo real, contraseña segura);
  // al iniciar sesión sólo el formato del correo y que haya contraseña (no se revelan las reglas de las cuentas).
  const errores: Partial<Record<CampoForm, string | null>> = {
    nombre: modo === "registro" ? validarNombre(nombre) : null,
    correo: validarCorreo(correo, { estricto: modo === "registro" }),
    clave: modo === "registro" ? validarClaveNueva(clave, correo) : validarClaveLogin(clave),
  };
  const errorDe = (c: CampoForm) => (tocados[c] ? errores[c] : null) ?? undefined;
  const tocar = (c: CampoForm) => setTocados((t) => ({ ...t, [c]: true }));
  const abierto = useRef(0);
  const idBase = useId();
  useEffect(() => {
    abierto.current = ahoraMs();
  }, []);

  const terminar = (rol: Rol, nombreUsuario: string) => {
    cerrarLogin();
    // El personal no compra: su espacio de trabajo es el panel. El cliente se queda donde estaba.
    if (rol !== "cliente") router.push("/panel");
    else toast(`Sesión iniciada. Sigues donde estabas, ${nombreUsuario.split(" ")[0]}.`, "ok");
  };

  // Capas 1 y 2 (locales) y 3 (reCAPTCHA verificado en el servidor). Devuelve un mensaje de error o null.
  const antiBot = async (accion: "login" | "registro"): Promise<string | null> => {
    if (trampa) return "Detectamos actividad automatizada. Intenta de nuevo más tarde.";
    if (msDesde(abierto.current) < TIEMPO_MINIMO_MS) return "Detectamos actividad automatizada (envío demasiado rápido).";
    const r = await verificar(accion);
    return r.ok ? null : r.error!;
  };

  const enviar = async (e: React.FormEvent) => {
    e.preventDefault();
    if (verificando) return;
    setError("");
    const c = normalizarCorreo(correo);
    // Validación de formato antes de cualquier otra cosa: se marcan todos los campos y se enfoca el primero con error.
    setTocados({ nombre: true, correo: true, clave: true });
    const primero = (["nombre", "correo", "clave"] as const).find((k) => errores[k]);
    if (primero) {
      document.getElementById(`${idBase}-${primero}`)?.focus();
      return;
    }

    setVerificando(true);
    try {
      const bot = await antiBot(modo);
      if (bot) return setError(bot);
      // Las credenciales se procesan sólo después de superar la verificación anti-bots, y se verifican en el servidor
      // (bcrypt). Tras varios intentos fallidos el servidor bloquea la cuenta temporalmente (RF-43).
      if (modo === "registro") {
        const r = await registrarCliente(normalizarNombre(nombre), c, clave);
        if (!r.ok) return setError(r.error!);
        toast(`Cuenta creada con ${PARAMETROS.bonoBienvenida} créditos de bienvenida.`, "ok");
        return terminar(r.usuario!.rol, r.usuario!.nombre);
      }
      const r = await iniciarSesion(c, clave);
      if (!r.ok) {
        setClave("");
        return setError(r.error!);
      }
      terminar(r.usuario!.rol, r.usuario!.nombre);
    } finally {
      setVerificando(false);
    }
  };

  return (
    <form onSubmit={enviar} className="space-y-4" autoComplete="on" noValidate>
      <div>
        <p className="etiqueta text-oro">Bienvenido a Cineight</p>
        <h2 className="titulo mt-1 text-2xl">{modo === "login" ? "Iniciar sesión" : "Crear cuenta"}</h2>
        <p className="mt-1 text-xs text-tenue">Al terminar seguirás en esta misma página.</p>
      </div>
      <div className="flex rounded-md border border-borde bg-fondo p-1" role="tablist">
        {(["login", "registro"] as const).map((x) => (
          <button
            type="button"
            role="tab"
            aria-selected={modo === x}
            key={x}
            onClick={() => {
              setModo(x);
              setError("");
              setTocados({});
            }}
            className={`flex flex-1 items-center justify-center gap-2 rounded py-2 text-[11px] font-bold uppercase tracking-[0.14em] transition-colors ${modo === x ? "border border-rojo/60 bg-vino text-salmon" : "text-tenue hover:text-tinta"}`}
          >
            {x === "login" ? <LogIn aria-hidden className="h-3.5 w-3.5" /> : <UserPlus aria-hidden className="h-3.5 w-3.5" />}
            {x === "login" ? "Iniciar sesión" : "Registrarse"}
          </button>
        ))}
      </div>
      {modo === "registro" && (
        <Campo
          id={`${idBase}-nombre`}
          label="Nombre y apellido"
          value={nombre}
          onChange={setNombre}
          onBlur={() => {
            setNombre((v) => normalizarNombre(v));
            tocar("nombre");
          }}
          error={errorDe("nombre")}
          autoComplete="name"
          maxLength={NOMBRE_MAX}
          placeholder="Ej. Ana López"
        />
      )}
      <Campo
        id={`${idBase}-correo`}
        label="Correo electrónico"
        value={correo}
        onChange={(v) => setCorreo(v.replace(/\s/g, ""))}
        onBlur={() => {
          setCorreo((v) => normalizarCorreo(v));
          tocar("correo");
        }}
        error={errorDe("correo")}
        type="email"
        inputMode="email"
        autoComplete="email"
        maxLength={CORREO_MAX}
        placeholder="nombre@dominio.com"
      />
      <Campo
        id={`${idBase}-clave`}
        label="Contraseña"
        value={clave}
        onChange={setClave}
        onBlur={() => tocar("clave")}
        error={errorDe("clave")}
        type={verClave ? "text" : "password"}
        autoComplete={modo === "login" ? "current-password" : "new-password"}
        maxLength={CLAVE_MAX}
        accion={
          <button
            type="button"
            onClick={() => setVerClave((v) => !v)}
            aria-label={verClave ? "Ocultar contraseña" : "Mostrar contraseña"}
            title={verClave ? "Ocultar contraseña" : "Mostrar contraseña"}
            className="grid h-full w-10 place-items-center text-tenue transition-colors hover:text-tinta"
          >
            {verClave ? <EyeOff aria-hidden className="h-4 w-4" /> : <Eye aria-hidden className="h-4 w-4" />}
          </button>
        }
      />
      {modo === "registro" && (
        <ul className="-mt-2 grid gap-1 sm:grid-cols-2" aria-label="Requisitos de la contraseña">
          {requisitosClave(clave, correo).map((r) => (
            <li key={r.texto} className={`flex items-center gap-1.5 text-[11px] ${r.ok ? "text-oro" : "text-apagado"}`}>
              {r.ok ? <Check aria-hidden className="h-3 w-3 shrink-0" /> : <Circle aria-hidden className="h-3 w-3 shrink-0" />}
              {r.texto}
            </li>
          ))}
        </ul>
      )}
      {/* Campo trampa: oculto para personas y lectores de pantalla; los bots suelen rellenarlo */}
      <input type="text" name="sitio_web" tabIndex={-1} autoComplete="off" aria-hidden value={trampa} onChange={(e) => setTrampa(e.target.value)} className="absolute -left-[9999px] h-0 w-0 opacity-0" />

      {error && <p className="rounded-md border border-rojo/60 bg-vino/50 p-2.5 text-sm text-salmon">{error}</p>}
      <Boton type="submit" tamano="lg" icono={modo === "login" ? LogIn : UserPlus} cargando={verificando} className="w-full">
        {verificando ? "Verificando…" : modo === "login" ? "Entrar" : "Crear cuenta"}
      </Boton>
      <AvisoRecaptcha />
      {modo === "registro" && <p className="text-[11px] text-apagado">Cada cuenta nueva recibe {PARAMETROS.bonoBienvenida} créditos de bienvenida.</p>}
    </form>
  );
}

type PropsCampo = {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  onBlur?: () => void;
  error?: string;
  type?: string;
  inputMode?: "email" | "text";
  autoComplete?: string;
  maxLength?: number;
  placeholder?: string;
  /** Botón dentro del campo, a la derecha (p. ej. mostrar contraseña). */
  accion?: React.ReactNode;
};

/** Campo con mensaje de error accesible (aria-invalid + aria-describedby) debajo del input. */
function Campo({ id, label, value, onChange, onBlur, error, type = "text", inputMode, autoComplete, maxLength, placeholder, accion }: PropsCampo) {
  return (
    <div>
      <label htmlFor={id} className="etiqueta block">
        {label}
      </label>
      <div className="relative">
        <input
          id={id}
          type={type}
          value={value}
          inputMode={inputMode}
          autoComplete={autoComplete}
          maxLength={maxLength}
          placeholder={placeholder}
          spellCheck={false}
          autoCapitalize={type === "email" || inputMode === "email" ? "none" : undefined}
          aria-invalid={!!error}
          aria-describedby={error ? `${id}-error` : undefined}
          onChange={(e) => onChange(e.target.value)}
          onBlur={onBlur}
          className={`campo normal-case tracking-normal ${accion ? "pr-11" : ""} ${error ? "!border-rojo focus:!border-rojo" : ""}`}
        />
        {accion && <div className="absolute inset-y-0 right-0 mt-1">{accion}</div>}
      </div>
      {error && (
        <p id={`${id}-error`} role="alert" className="mt-1.5 text-xs font-semibold text-salmon">
          {error}
        </p>
      )}
    </div>
  );
}
