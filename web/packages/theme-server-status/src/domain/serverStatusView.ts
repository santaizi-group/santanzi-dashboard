import type { CycleTransfer, ResourceRecord, ServerHost, ServerState, ServerRecord } from '@santaizi/api'
import { isHostOnline } from '@santaizi/api'
import {
  buildPublicNoteView,
  decodeOrderLink,
  flagCode,
  publicLocation,
  type PublicNoteView,
} from './publicNoteView'
import { osLabel } from './osLogo'
import {
  formatTransfer,
  getCycleTransferStatusLevel,
  pickCycleSummary,
  remainingPercentOf,
  trafficUsage,
  type CycleStatusLevel,
  type TrafficUsageView,
} from './transfer'

export interface MetricView {
  percent: number
  used: number
  total: number
  usedLabel: string
  totalLabel: string
}

export interface TempView {
  name: string
  value: number
}

export interface CycleTransferView {
  policyId: number
  name: string
  direction: string
  usedBytes: number
  quotaBytes: number
  remainingBytes: number
  usedLabel: string
  quotaLabel: string
  remainingLabel: string
  usagePercent: number
  remainingPercent: number | null
  status: string
  statusLevel: CycleStatusLevel
}

export type CycleTransferMap = Map<number, CycleTransferView[]>

export interface ServerStatusView {
  id: number
  source: ServerRecord
  name: string
  group: string
  online: boolean
  flagCode: string
  location: string
  slogan: string
  publicNote: PublicNoteView
  orderLink: string
  cpu: MetricView
  memory: MetricView
  disk: MetricView
  gpu: MetricView | null
  gpuNames: string[]
  cpuModels: string[]
  cpuCoreCount: number
  platformLabel: string
  speedIn: number
  speedOut: number
  speedInLabel: string
  speedOutLabel: string
  transferIn: number
  transferOut: number
  transferInLabel: string
  transferOutLabel: string
  trafficUsage: TrafficUsageView
  cycles: CycleTransferView[]
  cycleSummary: CycleTransferView | null
  uptimeSeconds: number
  uptimeLabel: string
  load1: number
  load5: number
  load15: number
  hasLoad: boolean
  platform: string
  platformVersion: string
  arch: string
  virtualization: string
  agentVersion: string
  bootTime: number
  lastActive: string
  lastActiveLabel: string
  swap: MetricView | null
  tcp: number | null
  udp: number | null
  processes: number | null
  temperatures: TempView[]
  available: boolean | null
  hasSpecs: boolean
}

function finite(value: unknown, fallback = 0) {
  const n = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(n) ? n : fallback
}

type FieldSource = ServerHost | ServerState | Record<string, unknown> | undefined

export function pick(object: FieldSource, ...keys: string[]) {
  if (!object) return undefined
  const source = object as Record<string, unknown>
  for (const key of keys) {
    const value = source[key]
    if (value !== undefined && value !== null) return value
  }
  return undefined
}

export function pickNumber(object: FieldSource, ...keys: string[]) {
  return finite(pick(object, ...keys))
}

export function pickOptionalNumber(object: FieldSource, ...keys: string[]) {
  const value = pick(object, ...keys)
  if (value === undefined || value === null || value === '') return null
  return finite(value)
}

/** 列表单元格：两侧都缺能力时返回空串，由调用方显示 —；有一侧则另一侧按 0。 */
export function formatConnPair(tcp: number | null, udp: number | null) {
  if (tcp === null && udp === null) return ''
  return `${Math.round(tcp ?? 0)} / ${Math.round(udp ?? 0)}`
}

export function pickText(object: FieldSource, ...keys: string[]) {
  const value = pick(object, ...keys)
  if (value === undefined || value === null) return ''
  return String(value).trim()
}

export function clampPercent(value: number) {
  return Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
}

export function percentOf(used: number, total?: number) {
  return clampPercent(total && total > 0 ? (used / total) * 100 : used)
}

export function formatBytes(value: number, decimals = 1) {
  let n = Math.max(0, finite(value))
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < 4) {
    n /= 1024
    i += 1
  }
  return `${n.toFixed(i ? decimals : 0)} ${units[i]}`
}

function metric(used: number, total: number, usedIsPercent = false): MetricView {
  const percent = usedIsPercent ? clampPercent(used) : percentOf(used, total || undefined)
  return {
    percent,
    used,
    total,
    usedLabel: formatTransfer(used),
    totalLabel: formatTransfer(total),
  }
}

function textList(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  if (typeof value === 'string' && value.trim()) return [value.trim()]
  return []
}

export function parseCpuCores(cpu: unknown) {
  const texts = textList(cpu)
  let cores = 0
  for (const text of texts) {
    const match = text.match(/(\d+)\s*(Virtual|Physics|Physical)\s*Core/i)
    if (match) cores += Number(match[1])
  }
  return cores
}

function temperatures(raw: unknown): TempView[] {
  if (!Array.isArray(raw)) return []
  const rows: TempView[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const row = item as Record<string, unknown>
    const value = finite(pick(row, 'Temperature'))
    const name = pickText(row, 'Name')
    if (!name && value === 0) continue
    rows.push({ name, value })
  }
  return rows
}

function readServerId(row: ResourceRecord | CycleTransfer) {
  const rec = row as ResourceRecord
  return Math.trunc(finite(row.server_id ?? rec.serverId ?? rec.ServerID))
}

export function formatUptime(seconds: number, locale = 'zh-CN') {
  const total = Math.max(0, Math.floor(seconds))
  const days = Math.floor(total / 86400)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60)
  const unit = days > 0 ? 'day' : hours > 0 ? 'hour' : 'minute'
  const value = days > 0 ? days : hours > 0 ? hours : minutes
  return new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'short' }).format(value)
}

export function formatTimestamp(value: unknown, locale = 'zh-CN') {
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
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

function resolveUptimeSeconds(state: ServerState | undefined, bootTime: number, nowMs: number) {
  const uptime = pickNumber(state, 'Uptime')
  if (uptime > 0) return uptime
  if (!bootTime) return 0
  const bootMs = bootTime > 1e12 ? bootTime : bootTime * 1000
  return Math.max(0, Math.floor((nowMs - bootMs) / 1000))
}

export function mapCycleTransfers(rows: Array<ResourceRecord | CycleTransfer>): CycleTransferMap {
  const result = new Map<number, CycleTransferView[]>()
  for (const row of rows) {
    const rec = row as ResourceRecord
    const serverId = readServerId(row)
    if (!serverId) continue
    const usedBytes = Math.max(0, finite(row.used_bytes ?? rec.usedBytes))
    const quotaBytes = Math.max(0, finite(row.quota_bytes ?? rec.quotaBytes))
    const remainingRaw = row.remaining_bytes ?? rec.remainingBytes
    const remainingBytes = remainingRaw === undefined || remainingRaw === null
      ? Math.max(quotaBytes - usedBytes, 0)
      : Math.max(0, finite(remainingRaw))
    const usagePercent = quotaBytes > 0
      ? percentOf(usedBytes, quotaBytes)
      : clampPercent(finite(row.usage_percent ?? rec.usagePercent))
    const remainingPercent = remainingPercentOf(usedBytes, quotaBytes, finite(row.usage_percent ?? rec.usagePercent))
    const list = result.get(serverId) || []
    list.push({
      policyId: Math.trunc(finite(row.policy_id ?? rec.policyId ?? rec.id)),
      name: String(row.name || ''),
      direction: String(row.direction || rec.mode || ''),
      usedBytes,
      quotaBytes,
      remainingBytes,
      usedLabel: formatTransfer(usedBytes),
      quotaLabel: formatTransfer(quotaBytes),
      remainingLabel: quotaBytes > 0 ? formatTransfer(remainingBytes) : '',
      usagePercent,
      remainingPercent,
      status: String(row.status || 'normal'),
      statusLevel: getCycleTransferStatusLevel(remainingPercent),
    })
    result.set(serverId, list)
  }
  return result
}

export function toServerStatusView(
  server: ServerRecord,
  cycles?: CycleTransferMap,
  nowMs = Date.now(),
  locale = 'zh-CN',
): ServerStatusView {
  const state = server.state || {}
  const host = server.host || {}
  const publicNote = buildPublicNoteView(server.public_note, nowMs)
  const memUsed = pickNumber(state, 'MemUsed')
  const memTotal = pickNumber(host, 'MemTotal')
  const diskUsed = pickNumber(state, 'DiskUsed')
  const diskTotal = pickNumber(host, 'DiskTotal')
  const swapUsed = pickNumber(state, 'SwapUsed')
  const swapTotal = pickNumber(host, 'SwapTotal')
  const gpuPercent = pickNumber(state, 'GPU')
  const gpuNames = textList(pick(host, 'GPU'))
  const cpuRaw = pick(host, 'CPU')
  const cpuModels = textList(cpuRaw).filter((item) => Number.isNaN(Number(item)) || item.includes(' '))
  const load1 = pickNumber(state, 'Load1')
  const load5 = pickNumber(state, 'Load5')
  const load15 = pickNumber(state, 'Load15')
  const tcp = pickOptionalNumber(state, 'TcpConnCount')
  const udp = pickOptionalNumber(state, 'UdpConnCount')
  const processes = pickOptionalNumber(state, 'ProcessCount')
  const temps = temperatures(pick(state, 'Temperatures'))
  const agentVersion = pickText(host, 'Version')
  const bootTime = pickNumber(host, 'BootTime')
  const lastActive = String(server.last_active || '')
  const available = server.telemetry?.available ?? null
  const swap = swapTotal > 0 ? metric(swapUsed, swapTotal) : null
  const gpu = gpuPercent > 0 ? metric(gpuPercent, 0, true) : null
  const transferIn = pickNumber(state, 'NetInTransfer')
  const transferOut = pickNumber(state, 'NetOutTransfer')
  const speedIn = pickNumber(state, 'NetInSpeed')
  const speedOut = pickNumber(state, 'NetOutSpeed')
  const country = pickText(host, 'CountryCode')
  const platform = pickText(host, 'Platform')
  const cycleRows = cycles?.get(server.id) || []
  const cycleSummary = pickCycleSummary(cycleRows)
  const uptimeSeconds = resolveUptimeSeconds(state, bootTime, nowMs)
  const hasSpecs = Boolean(
    cpuModels.length
    || gpuNames.length
    || swap
    || tcp !== null
    || udp !== null
    || processes !== null
    || temps.length
    || agentVersion
    || bootTime
    || lastActive,
  )
  return {
    id: server.id,
    source: server,
    name: server.name,
    group: server.tag || 'default',
    online: isHostOnline(server),
    flagCode: flagCode(server.public_note, country),
    location: publicLocation(server.public_note, country, locale),
    slogan: publicNote.presentation.slogan,
    publicNote,
    orderLink: decodeOrderLink(publicNote.presentation.orderLink),
    cpu: metric(pickNumber(state, 'CPU'), 0, true),
    memory: metric(memUsed, memTotal),
    disk: metric(diskUsed, diskTotal),
    gpu,
    gpuNames,
    cpuModels,
    cpuCoreCount: parseCpuCores(cpuRaw),
    platformLabel: osLabel(platform),
    speedIn,
    speedOut,
    speedInLabel: formatTransfer(speedIn) + '/s',
    speedOutLabel: formatTransfer(speedOut) + '/s',
    transferIn,
    transferOut,
    transferInLabel: formatTransfer(transferIn),
    transferOutLabel: formatTransfer(transferOut),
    trafficUsage: trafficUsage(
      transferIn,
      transferOut,
      publicNote.bill.trafficType,
      cycleSummary ? { remainingLabel: cycleSummary.remainingLabel, statusLevel: cycleSummary.statusLevel } : null,
    ),
    cycles: cycleRows,
    cycleSummary,
    uptimeSeconds,
    uptimeLabel: uptimeSeconds > 0 ? formatUptime(uptimeSeconds, locale) : '',
    load1,
    load5,
    load15,
    hasLoad: pick(state, 'Load1', 'Load5', 'Load15') !== undefined,
    platform,
    platformVersion: pickText(host, 'PlatformVersion'),
    arch: pickText(host, 'Arch'),
    virtualization: pickText(host, 'Virtualization'),
    agentVersion,
    bootTime,
    lastActive,
    lastActiveLabel: formatTimestamp(lastActive, locale),
    swap,
    tcp,
    udp,
    processes,
    temperatures: temps,
    available,
    hasSpecs,
  }
}

export function toServerStatusViews(
  servers: ServerRecord[],
  cycles?: CycleTransferMap,
  nowMs = Date.now(),
  locale = 'zh-CN',
) {
  return servers.map((server) => toServerStatusView(server, cycles, nowMs, locale))
}
