export type Translate = (key: string, ...args: unknown[]) => string

export function formatBytes(value: unknown, locale: string) {
  let bytes = Number(value || 0)
  if (!Number.isFinite(bytes)) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let index = 0
  while (Math.abs(bytes) >= 1024 && index < units.length - 1) {
    bytes /= 1024
    index++
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: index ? 1 : 0 }).format(bytes)} ${units[index]}`
}

export function formatLatencyMs(value: unknown, locale: string) {
  const ms = Number(value)
  if (!Number.isFinite(ms) || ms < 0) return '—'
  if (ms < 1000) {
    return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(ms)} ms`
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(ms / 1000)} s`
}

function asDate(value: unknown): Date | null {
  if (value === null || value === undefined || value === '') return null
  if (typeof value === 'number' && Number.isFinite(value)) {
    const ms = value > 1e15 ? value / 1e6 : value > 1e11 ? value : value * 1000
    const date = new Date(ms)
    return Number.isNaN(date.valueOf()) ? null : date
  }
  const text = String(value).trim()
  if (/^\d{16,}$/.test(text)) {
    const nano = Number(text)
    if (Number.isFinite(nano)) {
      const date = new Date(nano / 1e6)
      if (!Number.isNaN(date.valueOf())) return date
    }
  }
  const date = new Date(text)
  return Number.isNaN(date.valueOf()) ? null : date
}

export function formatProductVersion(raw?: string) {
  const value = (raw || '').trim()
  if (!value) return ''
  if (/^v/i.test(value)) return value
  if (/^\d/.test(value)) return `v${value}`
  return value
}

export function formatClockTime(value: unknown, locale: string) {
  if (value === null || value === undefined || value === '') return ''
  const date = asDate(value)
  if (!date || date.getUTCFullYear() <= 1) return ''
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date)
}

export function formatDateTime(value: unknown, locale: string) {
  if (value === null || value === undefined || value === '') return '—'
  const date = asDate(value)
  if (!date) return String(value)
  if (date.getUTCFullYear() <= 1) return '—'
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

export function formatLabel(value: string, t: Translate, te: (key: string) => boolean) {
  const folded = value.trim().replace(/\s+/g, '_').toLowerCase()
  if (te(folded)) return t(folded)
  if (te(value)) return t(value)
  return value
}

export function formatAdminValue(value: unknown, key: string, locale: string, t: Translate, te: (key: string) => boolean) {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'boolean') return t(value ? 'yes' : 'no')
  if (Array.isArray(value)) return value.join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  if (/(?:bytes|spool_size)$/.test(key)) return formatBytes(value, locale)
  if (/(?:rtt_ms|latency_ms|min_ms|avg_ms|max_ms)$/.test(key)) return formatLatencyMs(value, locale)
  if (/(?:_at|_from|_to|last_seen|last_active|last_sync|last_primary_seen|oldest_pending|bucket_start)$/.test(key)) return formatDateTime(value, locale)
  if (typeof value === 'string') return formatLabel(value, t, te)
  return String(value)
}

export interface ExtractedAPIError {
  code: string
  status?: number
  traceId?: string
  fields?: Record<string, string[]>
  detail?: string
  message?: string
}

function isOpaqueErrorCode(code: string) {
  return !code || /^\d+$/.test(code) || /^ERR_/.test(code) || /^(ECONN|ETIMEDOUT|EPIPE|ENOTFOUND)/.test(code)
}

export function extractAPIError(error: unknown): ExtractedAPIError {
  if (typeof error !== 'object' || error === null) return { code: '' }
  const record = error as {
    code?: unknown
    error_code?: unknown
    status?: unknown
    traceId?: unknown
    fields?: unknown
    message?: unknown
    detail?: unknown
  }
  let code = record.code != null && record.code !== '' ? String(record.code) : ''
  if (!code && record.error_code != null && record.error_code !== '') code = String(record.error_code)
  const status = typeof record.status === 'number' ? record.status : undefined
  const traceId = record.traceId != null ? String(record.traceId) : undefined
  const fields = record.fields && typeof record.fields === 'object'
    ? record.fields as Record<string, string[]>
    : undefined
  const message = record.message != null ? String(record.message) : undefined
  const detail = record.detail != null ? String(record.detail) : message
  return { code, status, traceId, fields, detail, message }
}

function truncateErrorDetail(value: string, max = 180) {
  const text = value.trim()
  if (text.length <= max) return text
  return `${text.slice(0, max - 1)}…`
}

export function formatAPIError(error: unknown, t: Translate, te: (key: string) => boolean) {
  const { code, detail } = extractAPIError(error)
  const key = `errors.${code}`
  const extra = detail ? truncateErrorDetail(detail) : ''
  if (code && te(key)) {
    const localized = t(key)
    if (extra && extra !== localized) return `${localized}：${extra}`
    return localized
  }
  if (extra) return extra
  return code && !isOpaqueErrorCode(code) ? t('requestFailedWithCode', { code }) : t('loadFailed')
}
