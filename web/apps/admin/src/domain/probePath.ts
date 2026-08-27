import type { ProbePath, ProbeSampleBucket, ProbeTCP } from '@santaizi/api'
import { formatLatencyMs } from '@/composables/format'

export type ProbeMetricTone = '' | 'is-ok' | 'is-fail'

export type ProbeMetric = {
  text: string
  tone: ProbeMetricTone
  port?: number
}

export interface ProbeLatencyStats {
  min?: number
  max?: number
  avg?: number
}

export function probeLatencyStats(points: ProbeSampleBucket[]): ProbeLatencyStats {
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  let weightedSum = 0
  let weight = 0

  for (const point of points) {
    const count = Number(point.success_count)
    if (!Number.isFinite(count) || count <= 0) continue

    const pointMin = Number(point.min_ms)
    const pointMax = Number(point.max_ms)
    const pointAvg = Number(point.avg_ms)
    if (Number.isFinite(pointMin)) min = Math.min(min, pointMin)
    if (Number.isFinite(pointMax)) max = Math.max(max, pointMax)
    if (Number.isFinite(pointAvg)) {
      weightedSum += pointAvg * count
      weight += count
    }
  }

  return {
    min: Number.isFinite(min) ? min : undefined,
    max: Number.isFinite(max) ? max : undefined,
    avg: weight > 0 ? weightedSum / weight : undefined,
  }
}

export function probePathKey(path: ProbePath) {
  return `${path.server_id}:${path.collector_id}`
}

export function probeHasNoTarget(path: ProbePath) {
  return path.target?.source === 'none'
}

export function probeTargetText(path: ProbePath) {
  return path.target?.hostname || path.target?.ipv4 || path.target?.ipv6 || '—'
}

export function formatProbeLoss(value: number | null | undefined, locale: string) {
  if (value == null || !Number.isFinite(value)) return '—'
  const percent = value <= 1 ? value * 100 : value
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(percent)}%`
}

export function probeLossPercent(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value <= 1 ? value * 100 : value))
}

export function probeHasICMP(path: ProbePath) {
  const icmp = path.icmp
  if (!icmp) return false
  return icmp.ok || (icmp.packets_sent ?? 0) > 0
}

export function probeBestTCP(path: ProbePath): ProbeTCP | undefined {
  const ok = (path.tcp || []).filter(item => item.ok)
  if (!ok.length) return undefined
  return ok.reduce((best, item) => (item.rtt_ms ?? Number.POSITIVE_INFINITY) < (best.rtt_ms ?? Number.POSITIVE_INFINITY) ? item : best)
}

export function probeICMPMetric(path: ProbePath, locale: string, timeout: string): ProbeMetric {
  if (probeHasNoTarget(path) || !probeHasICMP(path)) return { text: '—', tone: '' }
  if (path.icmp?.ok) return { text: formatLatencyMs(path.icmp.rtt_ms, locale), tone: 'is-ok' }
  if (!path.sampled_at) return { text: '—', tone: '' }
  return { text: timeout, tone: 'is-fail' }
}

export function probeTCPMetric(path: ProbePath, locale: string, timeout: string): ProbeMetric {
  const tcp = path.tcp || []
  if (!tcp.length) return { text: '—', tone: '' }
  const best = probeBestTCP(path)
  if (best) {
    return {
      text: formatLatencyMs(best.rtt_ms, locale),
      tone: 'is-ok',
      port: tcp.length === 1 ? best.port : undefined,
    }
  }
  if (!path.sampled_at) return { text: '—', tone: '' }
  return { text: timeout, tone: 'is-fail' }
}

export function probeMTRMetric(path: ProbePath, locale: string): ProbeMetric {
  const hops = path.mtr?.hop_count ?? 0
  if (hops <= 0) return { text: '—', tone: '' }
  const loss = path.mtr?.loss ?? 0
  return { text: formatProbeLoss(loss, locale), tone: loss > 0 ? 'is-fail' : 'is-ok' }
}

export function hopGeoText(hop: { private?: boolean; geo?: string }, privateLabel: string) {
  if (hop.private) return privateLabel
  return hop.geo || ''
}

export function formatHopASN(asn?: string | null) {
  const raw = (asn || '').trim()
  if (!raw) return ''
  const number = raw.replace(/^as/i, '').trim()
  return number ? `AS${number.toUpperCase()}` : ''
}

export function hopGeoLine(hop: { private?: boolean; geo?: string; asn?: string }, privateLabel: string) {
  const geo = hopGeoText(hop, privateLabel)
  const asn = formatHopASN(hop.asn)
  if (!asn || hop.private) return geo
  if (geo.toUpperCase().includes(asn.toUpperCase())) return geo
  return geo ? `${geo} · ${asn}` : asn
}

export type ProbeHopSource = {
  ttl: number
  address?: string
  hostname?: string
  geo?: string
  asn?: string
  private?: boolean
  avg_ms?: number
  rtt_ms?: number
  loss?: number
}

export type ProbeHopView = {
  ttl: number
  address: string
  hostname: string
  geo: string
  asn: string
  private: boolean
  rttMs?: number
  loss?: number
}

export type ProbeHopTone = '' | 'is-ok' | 'is-warn' | 'is-fail'

export type ProbeHopRouteStats = {
  hopCount: number
  maxRttMs?: number
  lastIndex: number
  lastRttMs?: number
  lastLoss?: number
  reached: boolean
}

function finiteRtt(value: unknown): number | undefined {
  const ms = Number(value)
  if (!Number.isFinite(ms) || ms < 0) return undefined
  return ms
}

export function normalizeProbeHops(hops?: ProbeHopSource[] | null): ProbeHopView[] {
  return (hops || []).map((hop) => ({
    ttl: hop.ttl,
    address: (hop.address || '').trim(),
    hostname: (hop.hostname || '').trim(),
    geo: hop.geo || '',
    asn: (hop.asn || '').trim(),
    private: Boolean(hop.private),
    rttMs: finiteRtt(hop.rtt_ms ?? hop.avg_ms),
    loss: hop.loss,
  }))
}

export function probeHopRouteStats(hops: ProbeHopView[]): ProbeHopRouteStats {
  let maxRttMs: number | undefined
  for (const hop of hops) {
    if (hop.rttMs == null) continue
    maxRttMs = maxRttMs == null ? hop.rttMs : Math.max(maxRttMs, hop.rttMs)
  }

  const lastIndex = hops.length - 1
  const last = lastIndex >= 0 ? hops[lastIndex] : undefined
  return {
    hopCount: hops.length,
    maxRttMs,
    lastIndex,
    lastRttMs: last?.rttMs,
    lastLoss: last?.loss,
    reached: Boolean(last?.address),
  }
}

export function probeHopRttShare(rttMs: number | undefined, maxRttMs: number | undefined) {
  if (rttMs == null || maxRttMs == null || maxRttMs <= 0) return 0
  return Math.min(100, Math.max(0, (rttMs / maxRttMs) * 100))
}

export function probeHopLossTone(loss: number | null | undefined): ProbeHopTone {
  if (loss == null || !Number.isFinite(loss)) return ''
  const percent = probeLossPercent(loss)
  if (percent <= 0) return 'is-ok'
  if (percent >= 50) return 'is-fail'
  return 'is-warn'
}

export function probeRouteChanged(
  current: Array<{ address?: string }>,
  previous?: Array<{ address?: string }> | null,
) {
  if (!previous?.length || !current.length) return false
  if (current.length !== previous.length) return true
  return current.some((hop, index) => (hop.address || '') !== (previous[index]?.address || ''))
}

export type ProbeRouteProtocol = 'icmp' | 'tcp'

export function defaultProbeRouteProtocol(trace?: { icmp?: { hops?: { loss?: number }[] }; tcp?: { hops?: unknown[] } } | null): ProbeRouteProtocol {
  const icmpHops = trace?.icmp?.hops || []
  const tcpHops = trace?.tcp?.hops || []
  if (!icmpHops.length && tcpHops.length) return 'tcp'
  if (icmpHops.length && tcpHops.length && probeLossPercent(icmpHops[icmpHops.length - 1]?.loss) >= 100) return 'tcp'
  return 'icmp'
}
