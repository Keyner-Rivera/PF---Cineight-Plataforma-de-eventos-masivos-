// Utilidades del visor del boleto. El código dinámico del QR lo firma el servidor (HMAC-SHA256 con un secreto que el
// navegador no conoce): aquí sólo se calcula qué minuto (ventana) corresponde a cada código.

export const ROTACION_S = 60; // el código dinámico del QR cambia cada minuto

// Oculta la parte central del token: nunca se muestra completo fuera del visor seguro.
export const enmascarar = (t: string) => `${t.slice(0, 6)}••••••${t.slice(-3)}`;

export const ventanaActual = (ms = Date.now()) => Math.floor(ms / (ROTACION_S * 1000));
