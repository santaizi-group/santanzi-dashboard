import { calcBinary, formatTransfer, splitTransfer } from '@santaizi/theme-server-status'

export { calcBinary, formatTransfer, splitTransfer }

function compactNumber(value: number, digits: number) {
  return Number(value.toFixed(digits))
}

/** 顶栏累计流量 / 网速：原版 header 一律 1 位小数。 */
export function formatHeaderStat(bytes: number) {
  const stats = calcBinary(bytes)
  if (stats.t > 1) return { value: compactNumber(stats.t, 1), unit: 'T' }
  if (stats.g > 1) return { value: compactNumber(stats.g, 1), unit: 'G' }
  if (stats.m > 1) return { value: compactNumber(stats.m, 1), unit: 'M' }
  return { value: compactNumber(stats.k, 1), unit: 'K' }
}

/** 卡片/列表网速：原版只有 G/M/K，1 位小数，不加 /s。 */
export function formatLiveSpeed(bytes: number) {
  const stats = calcBinary(bytes)
  if (stats.g > 1) return { value: compactNumber(stats.g, 1), unit: 'G' }
  if (stats.m > 1) return { value: compactNumber(stats.m, 1), unit: 'M' }
  return { value: compactNumber(stats.k, 1), unit: 'K' }
}

export function formatSpeed(bytes: number) {
  const { value, unit } = formatLiveSpeed(bytes)
  return `${value}${unit}`
}

/** 规格条：内存 >900MiB 取整 GiB，磁盘 >900GiB 取整 TiB，否则向上取整 GiB。 */
export function formatSpec(cores: number, memTotal: number, diskTotal: number) {
  const parts: string[] = []
  if (cores > 0) parts.push(`${cores}C`)
  if (memTotal > 0) {
    const mem = calcBinary(memTotal)
    parts.push(mem.m > 900 ? `${Math.round(mem.g)}G` : `${compactNumber(mem.g, 1)}G`)
  }
  if (diskTotal > 0) {
    const disk = calcBinary(diskTotal)
    parts.push(disk.g > 900 ? `${Math.round(disk.t)}T` : `${Math.ceil(disk.g)}G`)
  }
  return parts.join('/')
}

/** 表格规格列：核+内存。磁盘已有独立列，不塞进 80px。 */
export function formatTableSpec(cores: number, memTotal: number) {
  return formatSpec(cores, memTotal, 0)
}

export function formatDonutMem(used: number, total: number) {
  const usedStats = calcBinary(used)
  const totalStats = calcBinary(total)
  const value = usedStats.g >= 10 && totalStats.g >= 10
    ? `${compactNumber(usedStats.g, 1)}G`
    : `${Math.ceil(usedStats.m)}M`
  const totalLabel = totalStats.g > 4
    ? `${compactNumber(totalStats.g, 1)}G`
    : `${Math.ceil(totalStats.m)}M`
  return { value, totalLabel }
}

export function formatDonutDisk(used: number, total: number) {
  const usedStats = calcBinary(used)
  const totalStats = calcBinary(total)
  const value = usedStats.t >= 1 && totalStats.t >= 1
    ? `${compactNumber(usedStats.t, 1)}T`
    : `${Math.ceil(usedStats.g)}G`
  const totalLabel = totalStats.t >= 1
    ? `${compactNumber(totalStats.t, 1)}T`
    : `${Math.ceil(totalStats.g)}G`
  return { value, totalLabel }
}

export function formatDateTime(value: unknown, locale = 'zh-CN') {
  if (value === null || value === undefined || value === '') return ''
  let date: Date | null = null
  if (value instanceof Date) date = value
  else if (typeof value === 'number' && Number.isFinite(value)) {
    date = new Date(value > 1e12 ? value : value * 1000)
  } else {
    const parsed = Date.parse(String(value))
    if (!Number.isNaN(parsed)) date = new Date(parsed)
  }
  if (!date || date.getUTCFullYear() <= 1) return ''
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

/** @deprecated 实时流量请用 formatTransfer；保留给旧调用点。 */
export function formatBinary(value: number, _decimals = 1) {
  const text = formatTransfer(value)
  const { value: amount, unit } = splitTransfer(value)
  return { value: amount, unit, text }
}

export function formatPercent(current: number, total?: number) {
  const pct = total ? (100 * current) / total : current
  return `${Math.max(0, Math.min(100, pct)).toFixed(1)}%`
}
