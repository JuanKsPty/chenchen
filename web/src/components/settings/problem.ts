import { ApiError } from '@/api/client'

/**
 * What Ajustes and Reglas say when a request fails, in the app's voice.
 *
 *   problemMessage(error)  the server's own sentence for a 4xx (it is already
 *                          written for people: "La contraseña actual no es
 *                          correcta."), the generic line for a 5xx, and the
 *                          connection line when nothing came back at all.
 *   fieldProblem(error, name)  the server's message for one field, if it sent one.
 */

export const GENERIC_PROBLEM =
  'Algo falló de nuestro lado. Tus datos están bien; intenta otra vez en un momento.'

export const NETWORK_PROBLEM = 'No pudimos conectar. Revisa tu conexión e intenta otra vez.'

export function problemMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.status >= 500 ? GENERIC_PROBLEM : error.message
  }
  return NETWORK_PROBLEM
}

export function fieldProblem(error: unknown, name: string): string | undefined {
  return error instanceof ApiError ? error.fields?.[name] : undefined
}
