import { createApi, ApiError, describe } from '@go-tangra/ui/api'
import type { paths } from './schema.d'
export const BASE = '/api/asterisk'
export const api = createApi({ base: BASE })
export type ApiPath = keyof paths
export function explain(err: unknown): string {
 if (err instanceof ApiError) {
  if (err.status === 503) return 'This feature or its data source is unavailable.'
  if (err.status === 401) return 'Your session has ended. Sign in again.'
  if (err.status === 403) return 'You do not have access to this PBX or operation.'
  if (err.status === 400) return 'Check the period and filters, or choose a shorter period.'
  if (err.status === 404) return 'This record or recording is unavailable.'
 }
 return describe(err)
}
export async function get<T>(path: ApiPath, query: Record<string,string|number|boolean|undefined> = {}, params: Record<string,string> = {}, signal?: AbortSignal): Promise<T> {
 const target = path.replace(/\{([^}]+)\}/g, (_,key:string) => encodeURIComponent(params[key] ?? ''))
 return api<T>('GET', target, undefined, { query, ...(signal ? {signal} : {}) })
}
export function recordingURL(linkedid: string): string { return api.fileUrl('recordings/' + encodeURIComponent(linkedid)) }
