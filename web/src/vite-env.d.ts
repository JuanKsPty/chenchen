/**
 * The build-time variables the app reads. Vite writes them into the bundle when it
 * builds, so each is the same for everyone who loads that build, and none may be a
 * secret. Each is optional: a build without it works, with a little less.
 *
 *   VITE_SHORTCUT_URL  an https link (iCloud) to a ready-made shortcut for noting
 *                      purchases from the iPhone: "Obtener el atajo" in "Anotar
 *                      compras con Atajos". Without it, the steps alone.
 */
interface ImportMetaEnv {
  readonly VITE_SHORTCUT_URL?: string
}
