import type { CycleTransfer, ResourceRecord, SensorTemperature, ServerRecord } from '@santaizi/api'
import { isHostOnline } from '@santaizi/api'
import { buildPublicNoteView, decodeOrderLink, formatTransfer, osLabel, resolveFlagCode, type PublicNoteView } from '@santaizi/theme-server-status'
import { formatDonutDisk, formatDonutMem, formatSpec } from '../utils/host'
import { resolveServerLocation, type ServerLocation } from '../utils/worldMap'

export interface NazhuaCycleTransferView {
  name: string
  direction: string
  usedBytes: number
  quotaBytes: number
  remainingBytes: number
  usagePercent: number
  warningPercent: number
  status: string
  windowStart: string
  windowEnd: string
  nextResetAt: string
}

export type NazhuaCycleTransferMap = Map<number, NazhuaCycleTransferView>

export interface NazhuaTempView {
  name: string
  value: number
}

export interface NazhuaServerView {
  id: number
  source: ServerRecord
  name: string
  group: string
  online: boolean
  platform: string
  platformLabel: string
  platformVersion: string
  arch: string
  virtualization: string
  agentVersion: string
  cpuModels: string[]
  cpuCores: number
  gpuNames: string[]
  gpuPercent: number
  flagCode: string
  flagClass: string
  location: ServerLocation | null
  publicNote: PublicNoteView
  slogan: string
  spec: string
  cpuPercent: number
  memoryPercent: number
  diskPercent: number
  swapPercent: number
  memoryUsed: number
  memoryTotal: number
  diskUsed: number
  diskTotal: number
  swapUsed: number
  swapTotal: number
  memoryValue: string
  diskValue: string
  swapValue: string
  memoryTotalLabel: string
  diskTotalLabel: string
  swapTotalLabel: string
  memoryText: string
  diskText: string
  swapText: string
  memoryCaption: string
  diskCaption: string
  swapCaption: string
  cpuCaption: string
  uptimeSeconds: number
  uptime: string
  bootTime: number
  lastActive: string
  speedIn: number
  speedOut: number
  transferIn: number
  transferOut: number
  trafficBytes: number
  load1: number
  load5: number
  load15: number
  tcpConnCount: number
  udpConnCount: number
  processCount: number
  temperatures: NazhuaTempView[]
  cycle: NazhuaCycleTransferView | null
  billing: string
  orderLink: string
}

function finite(value: unknown) {
  const number = Number(value)
  return Number.isFinite(number) ? number : 0
}

function text(value: string | undefined) {
  return value ? value.trim() : ''
}

function textList(value: unknown) {
  if (Array.isArray(value)) return value.map(item => String(item).trim()).filter(Boolean)
  if (typeof value === 'string' && value.trim()) return [value.trim()]
  return []
}

export function parseCpuCores(cpu: unknown) {
  const texts = textList(cpu)
  let cores = 0
  for (const text of texts) {
    const match = text.match(/(\d+)\s+(Virtual|Physics|Physical)\s+Core/i)
    if (match) {
      cores += Number(match[1])
      continue
    }
    if (/^\d+(\.\d+)?$/.test(text)) cores += Number(text)
  }
  if (cores > 0) return cores
  if (typeof cpu === 'number' && Number.isFinite(cpu) && cpu > 0) return cpu
  return texts.length
}

export function clampPercent(value: number) {
  return Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
}

export function percentOf(used: number, total?: number) {
  return clampPercent(total && total > 0 ? (used / total) * 100 : used)
}

export function formatCompactBytes(value: number, _decimals = 0) {
  return formatTransfer(value)
}

export function formatUptime(seconds: number) {
  const safe = Math.max(0, Math.floor(seconds))
  const days = Math.floor(safe / 86_400)
  if (days > 0) return `${days}`
  const hours = Math.floor(safe / 3_600)
  if (hours > 0) return `${hours}h`
  const minutes = Math.floor(safe / 60)
  return `${minutes}m`
}

export function usageText(used: number, total: number, kind: 'mem' | 'disk' = 'mem') {
  const formatted = kind === 'disk' ? formatDonutDisk(used, total) : formatDonutMem(used, total)
  if (total > 0) return `${formatted.value} / ${formatted.totalLabel}`
  return formatted.value
}

export function usageCaption(used: number, total: number, percent: number, kind: 'mem' | 'disk' = 'mem') {
  return `${usageText(used, total, kind)} (${Number(percent.toFixed(1))}%)`
}

function planTrafficBytes(transferIn: number, transferOut: number, trafficType: string) {
  switch (Number(trafficType)) {
    case 1:
      return transferOut
    case 3:
      return transferOut >= transferIn ? transferOut : transferIn
    default:
      return transferIn + transferOut
  }
}

function readServerId(row: ResourceRecord | CycleTransfer) {
  return Math.trunc(finite(row.server_id ?? (row as ResourceRecord).serverId))
}

function temperatures(raw: SensorTemperature[] | undefined): NazhuaTempView[] {
  if (!Array.isArray(raw)) return []
  const rows: NazhuaTempView[] = []
  for (const item of raw) {
    const value = finite(item?.Temperature)
    const name = text(item?.Name)
    if (!name && value === 0) continue
    rows.push({ name: name || 'sensor', value })
  }
  return rows
}

export function mapCycleTransfers(rows: Array<ResourceRecord | CycleTransfer>): NazhuaCycleTransferMap {
  const result = new Map<number, NazhuaCycleTransferView>()
  for (const row of rows) {
    const serverId = readServerId(row)
    if (!serverId) continue
    const usedBytes = Math.max(0, finite(row.used_bytes ?? (row as ResourceRecord).usedBytes))
    const quotaBytes = Math.max(0, finite(row.quota_bytes ?? (row as ResourceRecord).quotaBytes))
    const remainingBytes = Math.max(0, finite(row.remaining_bytes) || publicRemaining(usedBytes, quotaBytes))
    const current = result.get(serverId)
    if (current) {
      current.usedBytes += usedBytes
      current.quotaBytes += quotaBytes
      current.remainingBytes += remainingBytes
      current.usagePercent = percentOf(current.usedBytes, current.quotaBytes)
      if (String(row.status || '') && row.status !== 'normal') current.status = String(row.status)
      continue
    }
    result.set(serverId, {
      name: String(row.name || ''),
      direction: String(row.direction || (row as ResourceRecord).mode || ''),
      usedBytes,
      quotaBytes,
      remainingBytes,
      usagePercent: quotaBytes > 0 ? percentOf(usedBytes, quotaBytes) : clampPercent(finite(row.usage_percent)),
      warningPercent: finite(row.warning_percent),
      status: String(row.status || 'normal'),
      windowStart: String(row.window_start || ''),
      windowEnd: String(row.window_end || ''),
      nextResetAt: row.next_reset_at ? String(row.next_reset_at) : '',
    })
  }
  return result
}

function publicRemaining(used: number, quota: number) {
  return quota > used ? quota - used : 0
}

function billingLabel(note: PublicNoteView) {
  if (note.bill.amountKind === 'free') return '0'
  if (note.bill.amountKind === 'metered') return '∞'
  return note.bill.amountValue
}

export function toNazhuaServerView(
  server: ServerRecord,
  cycles?: NazhuaCycleTransferMap,
  nowMs = Date.now(),
): NazhuaServerView {
  const state = server.state
  const host = server.host
  const memoryUsed = finite(state?.MemUsed)
  const memoryTotal = finite(host?.MemTotal)
  const diskUsed = finite(state?.DiskUsed)
  const diskTotal = finite(host?.DiskTotal)
  const swapUsed = finite(state?.SwapUsed)
  const swapTotal = finite(host?.SwapTotal)
  const transferIn = finite(state?.NetInTransfer)
  const transferOut = finite(state?.NetOutTransfer)
  const location = resolveServerLocation(server)
  const publicNote = buildPublicNoteView(server.public_note, nowMs)
  // 与 ServerStatus 同一套归一化：位置码可能是 IATA（LAX/HKG），flag-icons 只认 ISO2。
  const flagCode = resolveFlagCode(
    publicNote.presentation.flag,
    publicNote.presentation.location || location?.code,
    host?.CountryCode,
  )
  const cpuModels = textList(host?.CPU)
  const cpuCores = parseCpuCores(host?.CPU)
  const spec = formatSpec(cpuCores, memoryTotal, diskTotal)
  const cycle = cycles?.get(server.id) || null
  const memDonut = formatDonutMem(memoryUsed, memoryTotal)
  const diskDonut = formatDonutDisk(diskUsed, diskTotal)
  const swapDonut = formatDonutMem(swapUsed, swapTotal)
  const cpuPercent = percentOf(finite(state?.CPU))
  const memoryPercent = percentOf(memoryUsed, memoryTotal || undefined)
  const diskPercent = percentOf(diskUsed, diskTotal || undefined)
  const swapPercent = percentOf(swapUsed, swapTotal || undefined)
  return {
    id: server.id,
    source: server,
    name: server.name,
    group: server.tag || 'default',
    online: isHostOnline(server),
    platform: text(host?.Platform),
    platformLabel: osLabel(host?.Platform),
    platformVersion: text(host?.PlatformVersion),
    arch: text(host?.Arch),
    virtualization: text(host?.Virtualization),
    agentVersion: text(host?.Version),
    cpuModels,
    cpuCores,
    gpuNames: textList(host?.GPU),
    gpuPercent: finite(state?.GPU),
    flagCode,
    flagClass: flagCode ? `fi fi-${flagCode}` : '',
    location,
    publicNote,
    slogan: publicNote.presentation.slogan,
    spec,
    cpuPercent,
    memoryPercent,
    diskPercent,
    swapPercent,
    memoryUsed,
    memoryTotal,
    diskUsed,
    diskTotal,
    swapUsed,
    swapTotal,
    memoryValue: memDonut.value,
    diskValue: diskDonut.value,
    swapValue: swapDonut.value,
    memoryTotalLabel: memDonut.totalLabel,
    diskTotalLabel: diskDonut.totalLabel,
    swapTotalLabel: swapDonut.totalLabel,
    memoryText: usageText(memoryUsed, memoryTotal, 'mem'),
    diskText: usageText(diskUsed, diskTotal, 'disk'),
    swapText: usageText(swapUsed, swapTotal, 'mem'),
    memoryCaption: usageCaption(memoryUsed, memoryTotal, memoryPercent, 'mem'),
    diskCaption: usageCaption(diskUsed, diskTotal, diskPercent, 'disk'),
    swapCaption: usageCaption(swapUsed, swapTotal, swapPercent, 'mem'),
    cpuCaption: cpuCores > 0 ? `${cpuCores}C` : '',
    uptimeSeconds: finite(state?.Uptime),
    uptime: formatUptime(finite(state?.Uptime)),
    bootTime: finite(host?.BootTime),
    lastActive: server.last_active || '',
    speedIn: finite(state?.NetInSpeed),
    speedOut: finite(state?.NetOutSpeed),
    transferIn,
    transferOut,
    trafficBytes: cycle && cycle.quotaBytes > 0
      ? Math.max(cycle.quotaBytes - cycle.usedBytes, 0)
      : planTrafficBytes(transferIn, transferOut, publicNote.bill.trafficType),
    load1: finite(state?.Load1),
    load5: finite(state?.Load5),
    load15: finite(state?.Load15),
    tcpConnCount: finite(state?.TcpConnCount),
    udpConnCount: finite(state?.UdpConnCount),
    processCount: finite(state?.ProcessCount),
    temperatures: temperatures(state?.Temperatures),
    cycle,
    billing: billingLabel(publicNote),
    orderLink: decodeOrderLink(publicNote.presentation.orderLink),
  }
}

export function toNazhuaServerViews(servers: ServerRecord[], cycles?: NazhuaCycleTransferMap) {
  return servers.map(server => toNazhuaServerView(server, cycles))
}
