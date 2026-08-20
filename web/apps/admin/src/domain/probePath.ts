import type { ProbePath, ProbeTCP } from '@santaizi/api'
import { formatLatencyMs } from '@/composables/format'

export type ProbeMetricTone = '' | 'is-ok' | 'is-fail'

export type ProbeMetric = {
  text: string
  tone: ProbeMetricTone
  port?: number
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

export type ProbeRouteProtocol = 'icmp' | 'tcp'

export function defaultProbeRouteProtocol(trace?: { icmp?: { hops?: { loss?: number }[] }; tcp?: { hops?: unknown[] } } | null): ProbeRouteProtocol {
  const icmpHops = trace?.icmp?.hops || []
  const tcpHops = trace?.tcp?.hops || []
  if (!icmpHops.length && tcpHops.length) return 'tcp'
  if (icmpHops.length && tcpHops.length && probeLossPercent(icmpHops[icmpHops.length - 1]?.loss) >= 100) return 'tcp'
  return 'icmp'
}
