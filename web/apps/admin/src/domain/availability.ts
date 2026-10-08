export const DEFAULT_BUCKET_MS = 30_000

export type ObserverEvidence = {
  observer_id?: string
  observer_kind?: string
  observer_name?: string
  healthy?: boolean
  seen?: boolean
}

export type AvailabilityBucketLike = {
  bucket_start?: unknown
  window_end?: unknown
  host?: unknown
  connectivity?: unknown
  expected_observers?: unknown
  healthy_observers?: unknown
  seen_observers?: unknown
  observer_evidence?: unknown
  [key: string]: unknown
}

export type AvailabilitySegmentKind = 'observed' | 'gap'

export type AvailabilitySegment = {
  kind: AvailabilitySegmentKind
  host: string
  connectivity: string
  start: number
  end: number
  expectedObservers: number
  healthyObservers: number
  seenObservers: number
  observerEvidence: ObserverEvidence[]
}

export type AvailabilitySummary = {
  availableMs: number
  partialMs: number
  unavailableMs: number
  unknownMs: number
  gapMs: number
  availablePercent: number | null
  outageCount: number
  degradedCount: number
  windowStart: number | null
  windowEnd: number | null
}

function toEpochMs(value: unknown): number | null {
  if (value === null || value === undefined || value === '') return null
  if (typeof value === 'number' && Number.isFinite(value)) {
    const ms = value > 1e15 ? value / 1e6 : value > 1e11 ? value : value * 1000
    const date = new Date(ms)
    return Number.isNaN(date.valueOf()) ? null : date.valueOf()
  }
  const text = String(value).trim()
  const trimmed = text.replace(/(\.\d{3})\d+/, '$1')
  const date = new Date(trimmed)
  return Number.isNaN(date.valueOf()) ? null : date.valueOf()
}

function asNumber(value: unknown): number {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

function asText(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asEvidence(value: unknown): ObserverEvidence[] {
  if (!Array.isArray(value)) return []
  return value.map(item => {
    const row = (item || {}) as ObserverEvidence
    return {
      observer_id: asText(row.observer_id),
      observer_kind: asText(row.observer_kind),
      observer_name: asText(row.observer_name),
      healthy: !!row.healthy,
      seen: !!row.seen,
    }
  })
}

function seenObserverKey(evidence: ObserverEvidence[]): string {
  return evidence.filter(item => item.seen && item.observer_id).map(item => item.observer_id).sort().join(',')
}

function bucketSignature(host: string, connectivity: string, expected: number, healthy: number, seen: number, evidence: ObserverEvidence[]): string {
  return [host, connectivity, expected, healthy, seen, seenObserverKey(evidence)].join('|')
}

export function inferBucketMs(rows: AvailabilityBucketLike[]): number {
  const starts = rows.map(row => toEpochMs(row.bucket_start)).filter((value): value is number => value != null).sort((a, b) => a - b)
  let min = 0
  for (let i = 1; i < starts.length; i++) {
    const current = starts[i]
    const previous = starts[i - 1]
    if (current == null || previous == null) continue
    const diff = current - previous
    if (diff > 0 && (min === 0 || diff < min)) min = diff
  }
  return min || DEFAULT_BUCKET_MS
}

type parsedBucket = {
  start: number
  end: number
  host: string
  connectivity: string
  expectedObservers: number
  healthyObservers: number
  seenObservers: number
  observerEvidence: ObserverEvidence[]
  signature: string
}

function parseBucket(row: AvailabilityBucketLike, fallbackMs: number): parsedBucket | null {
  const start = toEpochMs(row.bucket_start)
  if (start == null) return null
  const explicitEnd = toEpochMs(row.window_end)
  const end = explicitEnd != null && explicitEnd > start ? explicitEnd : start + fallbackMs
  const host = asText(row.host) || 'unknown'
  const connectivity = asText(row.connectivity) || 'unknown'
  const expectedObservers = asNumber(row.expected_observers)
  const healthyObservers = asNumber(row.healthy_observers)
  const seenObservers = asNumber(row.seen_observers)
  const observerEvidence = asEvidence(row.observer_evidence)
  return {
    start, end, host, connectivity, expectedObservers, healthyObservers, seenObservers, observerEvidence,
    signature: bucketSignature(host, connectivity, expectedObservers, healthyObservers, seenObservers, observerEvidence),
  }
}

function gapSegment(start: number, end: number): AvailabilitySegment {
  return {
    kind: 'gap', host: 'unknown', connectivity: 'unknown', start, end,
    expectedObservers: 0, healthyObservers: 0, seenObservers: 0, observerEvidence: [],
  }
}

export function buildAvailabilitySegments(rows: AvailabilityBucketLike[], bucketMs = inferBucketMs(rows)): AvailabilitySegment[] {
  const size = bucketMs > 0 ? bucketMs : DEFAULT_BUCKET_MS
  const parsed = rows.map(row => parseBucket(row, size)).filter((row): row is parsedBucket => row != null)
  parsed.sort((a, b) => a.start - b.start)
  const segments: AvailabilitySegment[] = []
  for (const bucket of parsed) {
    const end = bucket.end
    const last = segments[segments.length - 1]
    if (last?.kind === 'observed' && last.host === bucket.host && bucketSignature(last.host, last.connectivity, last.expectedObservers, last.healthyObservers, last.seenObservers, last.observerEvidence) === bucket.signature && bucket.start <= last.end) {
      last.end = Math.max(last.end, end)
      continue
    }
    if (last && bucket.start > last.end) {
      segments.push(gapSegment(last.end, bucket.start))
    }
    segments.push({
      kind: 'observed',
      host: bucket.host,
      connectivity: bucket.connectivity,
      start: bucket.start,
      end,
      expectedObservers: bucket.expectedObservers,
      healthyObservers: bucket.healthyObservers,
      seenObservers: bucket.seenObservers,
      observerEvidence: bucket.observerEvidence,
    })
  }
  return segments
}

function connectivityKind(connectivity: string): 'available' | 'partial' | 'unavailable' | 'unknown' {
  if (connectivity === 'full') return 'available'
  if (connectivity === 'partial') return 'partial'
  if (connectivity === 'unavailable') return 'unavailable'
  return 'unknown'
}

export function summarizeAvailability(segments: AvailabilitySegment[]): AvailabilitySummary {
  const summary: AvailabilitySummary = {
    availableMs: 0, partialMs: 0, unavailableMs: 0, unknownMs: 0, gapMs: 0,
    availablePercent: null, outageCount: 0, degradedCount: 0, windowStart: null, windowEnd: null,
  }
  if (!segments.length) return summary
  const first = segments[0]
  const last = segments[segments.length - 1]
  summary.windowStart = first ? first.start : null
  summary.windowEnd = last ? last.end : null
  let inOutage = false
  let inDegraded = false
  for (const segment of segments) {
    const span = Math.max(0, segment.end - segment.start)
    if (segment.kind === 'gap') {
      summary.gapMs += span
      inOutage = false
      inDegraded = false
      continue
    }
    const kind = connectivityKind(segment.connectivity)
    if (kind === 'available') {
      summary.availableMs += span
      inOutage = false
      inDegraded = false
    } else if (kind === 'partial') {
      summary.partialMs += span
      inOutage = false
      if (!inDegraded) {
        summary.degradedCount++
        inDegraded = true
      }
    } else if (kind === 'unavailable') {
      summary.unavailableMs += span
      inDegraded = false
      if (!inOutage) {
        summary.outageCount++
        inOutage = true
      }
    } else {
      summary.unknownMs += span
      inOutage = false
      inDegraded = false
    }
  }
  const up = summary.availableMs + summary.partialMs
  const known = up + summary.unavailableMs
  if (known > 0) summary.availablePercent = (up / known) * 100
  return summary
}

export function formatDurationMs(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  if (hours) return minutes ? `${hours}h ${minutes}m` : `${hours}h`
  if (minutes) return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`
  return `${seconds}s`
}

export function coverageLabel(segment: AvailabilitySegment): string {
  if (segment.kind === 'gap' || segment.expectedObservers <= 0) return '—'
  return `${segment.seenObservers}/${segment.expectedObservers}`
}
