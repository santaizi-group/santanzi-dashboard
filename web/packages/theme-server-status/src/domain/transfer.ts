export type CycleStatusLevel = 'over' | 'alert' | 'warning' | 'fine' | 'neutral'
export type TrafficUsageKind = 'cycle' | 'out' | 'both' | 'maxOut' | 'maxIn' | 'unlimited'

export interface TrafficUsageView {
  kind: TrafficUsageKind
  valueLabel: string
  statusLevel: CycleStatusLevel | ''
}

const STATUS_RANK: Record<CycleStatusLevel, number> = {
  over: 0,
  alert: 1,
  warning: 2,
  fine: 3,
  neutral: 4,
}

function finite(value: unknown, fallback = 0) {
  const n = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(n) ? n : fallback
}

function stripTrailingZeros(value: string) {
  return String(Number(value))
}

/** 与 nazhua-front / aobobo `calcBinary` 相同：各档位独立换算，不截断。 */
export function calcBinary(bytes: number) {
  const n = Math.max(0, finite(bytes))
  const k = n / 1024
  const m = k / 1024
  const g = m / 1024
  const t = g / 1024
  return { k, m, g, t, p: t > 1000 ? t / 1024 : null as number | null }
}

/** 实时流量：T/G/M/K，阈值 `>1`，去掉尾零。 */
export function formatTransfer(bytes: number) {
  const stats = calcBinary(bytes)
  if (stats.t > 1) return `${stripTrailingZeros(stats.t.toFixed(2))}T`
  if (stats.g > 1) return `${stripTrailingZeros(stats.g.toFixed(2))}G`
  if (stats.m > 1) return `${stripTrailingZeros(stats.m.toFixed(1))}M`
  return `${stripTrailingZeros(stats.k.toFixed(1))}K`
}

export type BytesAxisUnit = 'T' | 'G' | 'M' | 'K'

/** 按满量程一次定档，避免同一根轴混用 T/G。 */
export function bytesAxisUnit(maxBytes: number): BytesAxisUnit {
  const stats = calcBinary(Math.max(0, finite(maxBytes)))
  if (stats.t > 1) return 'T'
  if (stats.g > 1) return 'G'
  if (stats.m > 1) return 'M'
  return 'K'
}

export function bytesAxisFormatter(maxBytes: number) {
  const unit = bytesAxisUnit(maxBytes)
  const divisor = unit === 'T' ? 1024 ** 4 : unit === 'G' ? 1024 ** 3 : unit === 'M' ? 1024 ** 2 : 1024
  const decimals = unit === 'T' || unit === 'G' ? 2 : 1
  return (value: number) => {
    const n = finite(value)
    if (n === 0) return '0'
    return `${stripTrailingZeros((n / divisor).toFixed(decimals))}${unit}`
  }
}

export function formatSpeed(bytesPerSecond: number) {
  return `${formatTransfer(bytesPerSecond)}/s`
}

export function splitTransfer(bytes: number) {
  const text = formatTransfer(bytes)
  const matched = text.match(/^(-?[0-9.]+)(.*)$/)
  return { value: matched?.[1] || text, unit: matched?.[2] || '' }
}

/**
 * 周期流量详情：nazhua-front / aobobo `formatBytes`。
 * 首页实时流量仍走 `formatTransfer`（短单位 T/G/M/K）。
 */
export function formatCycleBytes(bytes: number) {
  const n = finite(bytes)
  if (!Number.isFinite(n) || n <= 0) return '0B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let value = n
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  const decimals = value >= 100 ? 0 : value >= 10 ? 1 : 2
  const formatted = value.toFixed(decimals).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1')
  return `${formatted}${units[unitIndex]}`
}

export function truncateDecimal(value: number, digits = 2) {
  const n = finite(value, Number.NaN)
  if (!Number.isFinite(n)) return Number.NaN
  const multiplier = 10 ** digits
  return Math.trunc(n * multiplier) / multiplier
}

export function formatAvailabilityPercent(value: number | null | undefined) {
  if (value === null || value === undefined) return ''
  const n = truncateDecimal(value, 2)
  if (!Number.isFinite(n)) return ''
  return `${n.toFixed(2)}%`
}

export function getCycleTransferStatusLevel(remainingPercent: number | null | undefined): CycleStatusLevel {
  if (!Number.isFinite(remainingPercent ?? Number.NaN)) return 'neutral'
  const percent = remainingPercent as number
  if (percent === 0) return 'over'
  if (percent > 50) return 'fine'
  if (percent > 20) return 'warning'
  if (percent > 0) return 'alert'
  return 'over'
}

export function remainingPercentOf(usedBytes: number, quotaBytes: number, usagePercent?: number) {
  if (quotaBytes > 0) {
    return Math.max(Number((100 - (usedBytes / quotaBytes) * 100).toFixed(2)), 0)
  }
  if (Number.isFinite(usagePercent) && (usagePercent as number) > 0) {
    return Math.max(Number((100 - (usagePercent as number)).toFixed(2)), 0)
  }
  return null
}

export function trafficUsage(
  transferIn: number,
  transferOut: number,
  trafficType: string,
  cycle?: { remainingLabel: string; statusLevel: CycleStatusLevel } | null,
): TrafficUsageView {
  if (cycle) {
    return { kind: 'cycle', valueLabel: cycle.remainingLabel, statusLevel: cycle.statusLevel }
  }
  switch (Number(trafficType)) {
    case 1:
      return { kind: 'out', valueLabel: formatTransfer(transferOut), statusLevel: '' }
    case 3: {
      const isOut = transferOut >= transferIn
      return {
        kind: isOut ? 'maxOut' : 'maxIn',
        valueLabel: formatTransfer(isOut ? transferOut : transferIn),
        statusLevel: '',
      }
    }
    case 2:
      return { kind: 'both', valueLabel: formatTransfer(transferIn + transferOut), statusLevel: '' }
    default:
      return { kind: 'unlimited', valueLabel: '', statusLevel: '' }
  }
}

export function pickCycleSummary<T extends { statusLevel: CycleStatusLevel; policyId?: number }>(rows: T[]): T | null {
  if (!rows.length) return null
  const [first] = [...rows].sort((a, b) => {
    const rank = STATUS_RANK[a.statusLevel] - STATUS_RANK[b.statusLevel]
    if (rank !== 0) return rank
    return (a.policyId || 0) - (b.policyId || 0)
  })
  return first ?? null
}

export function usageTone(percent: number): 'ok' | 'warn' | 'danger' {
  if (percent >= 90) return 'danger'
  if (percent >= 70) return 'warn'
  return 'ok'
}
