import type { ServerRecord } from '@santaizi/api'
import { isHostOnline } from '@santaizi/api'

export type ServerSortProp =
  | 'display_index'
  | 'name'
  | 'online'
  | 'country_code'
  | 'platform'
  | 'boot_time'
  | 'net_in_speed'
  | 'net_out_speed'
  | 'net_in_transfer'
  | 'net_out_transfer'
  | 'total_transfer'
  | 'tcp_conn_count'
  | 'udp_conn_count'
  | 'total_conn_count'
  | 'load1'
  | 'cpu'
  | 'cpu_cores'
  | 'mem_used'
  | 'mem_total'
  | 'swap_used'
  | 'swap_total'
  | 'disk_used'
  | 'disk_total'
export type ServerSortOrder = 'asc' | 'desc'
export type ServerSortGroup = 'base' | 'host' | 'net' | 'conn' | 'load' | 'cap'

export interface ServerSortOption {
  prop: ServerSortProp
  labelKey: string
  group: ServerSortGroup
}

export const SERVER_SORT_OPTIONS: ServerSortOption[] = [
  { prop: 'display_index', labelKey: 'nazhua.sortWeight', group: 'base' },
  { prop: 'name', labelKey: 'nazhua.sortName', group: 'base' },
  { prop: 'online', labelKey: 'nazhua.sortOnline', group: 'base' },
  { prop: 'country_code', labelKey: 'nazhua.sortCountry', group: 'host' },
  { prop: 'platform', labelKey: 'nazhua.sortPlatform', group: 'host' },
  { prop: 'boot_time', labelKey: 'nazhua.sortUptime', group: 'host' },
  { prop: 'net_in_speed', labelKey: 'nazhua.sortNetInSpeed', group: 'net' },
  { prop: 'net_out_speed', labelKey: 'nazhua.sortNetOutSpeed', group: 'net' },
  { prop: 'net_in_transfer', labelKey: 'nazhua.sortNetInTransfer', group: 'net' },
  { prop: 'net_out_transfer', labelKey: 'nazhua.sortNetOutTransfer', group: 'net' },
  { prop: 'total_transfer', labelKey: 'nazhua.sortTotalTransfer', group: 'net' },
  { prop: 'tcp_conn_count', labelKey: 'nazhua.sortTcp', group: 'conn' },
  { prop: 'udp_conn_count', labelKey: 'nazhua.sortUdp', group: 'conn' },
  { prop: 'total_conn_count', labelKey: 'nazhua.sortTotalConn', group: 'conn' },
  { prop: 'load1', labelKey: 'nazhua.sortLoad', group: 'load' },
  { prop: 'cpu', labelKey: 'nazhua.sortCpu', group: 'load' },
  { prop: 'cpu_cores', labelKey: 'nazhua.sortCpuCores', group: 'load' },
  { prop: 'mem_used', labelKey: 'nazhua.sortMemUsed', group: 'cap' },
  { prop: 'mem_total', labelKey: 'nazhua.sortMemTotal', group: 'cap' },
  { prop: 'swap_used', labelKey: 'nazhua.sortSwapUsed', group: 'cap' },
  { prop: 'swap_total', labelKey: 'nazhua.sortSwapTotal', group: 'cap' },
  { prop: 'disk_used', labelKey: 'nazhua.sortDiskUsed', group: 'cap' },
  { prop: 'disk_total', labelKey: 'nazhua.sortDiskTotal', group: 'cap' },
]

const SORT_PROPS = new Set(SERVER_SORT_OPTIONS.map((option) => option.prop))

export interface ServerSortColumn {
  group: ServerSortGroup
  options: ServerSortOption[]
}

export const SERVER_SORT_COLUMNS: ServerSortColumn[] = SERVER_SORT_OPTIONS.reduce<ServerSortColumn[]>((columns, option) => {
  const current = columns[columns.length - 1]
  if (current && current.group === option.group) current.options.push(option)
  else columns.push({ group: option.group, options: [option] })
  return columns
}, [])

function textList(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
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

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function num(source: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const raw = source[key]
    if (raw !== undefined && raw !== null && raw !== '') {
      const n = typeof raw === 'number' ? raw : Number(raw)
      if (Number.isFinite(n)) return n
    }
  }
  return 0
}

function text(source: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const raw = source[key]
    if (raw !== undefined && raw !== null && String(raw).trim() !== '') return String(raw)
  }
  return ''
}

export function sortValue(server: ServerRecord, prop: ServerSortProp): number | string {
  const host = asRecord(server.host)
  const state = asRecord(server.state)
  switch (prop) {
    case 'name':
      return server.name
    case 'online':
      return Number(isHostOnline(server))
    case 'country_code':
      return text(host, 'CountryCode', 'country_code').toLowerCase()
    case 'platform':
      return text(host, 'Platform', 'platform').toLowerCase()
    case 'boot_time': {
      const boot = num(host, 'BootTime', 'boot_time')
      return boot > 0 ? Date.now() / 1000 - boot : 0
    }
    case 'net_in_speed':
      return num(state, 'NetInSpeed', 'net_in_speed')
    case 'net_out_speed':
      return num(state, 'NetOutSpeed', 'net_out_speed')
    case 'net_in_transfer':
      return num(state, 'NetInTransfer', 'net_in_transfer')
    case 'net_out_transfer':
      return num(state, 'NetOutTransfer', 'net_out_transfer')
    case 'total_transfer':
      return num(state, 'NetInTransfer', 'net_in_transfer') + num(state, 'NetOutTransfer', 'net_out_transfer')
    case 'tcp_conn_count':
      return num(state, 'TcpConnCount', 'tcp_conn_count')
    case 'udp_conn_count':
      return num(state, 'UdpConnCount', 'udp_conn_count')
    case 'total_conn_count':
      return num(state, 'TcpConnCount', 'tcp_conn_count') + num(state, 'UdpConnCount', 'udp_conn_count')
    case 'load1':
      return num(state, 'Load1', 'load1')
    case 'cpu':
      return num(state, 'CPU', 'Cpu', 'cpu')
    case 'cpu_cores':
      return parseCpuCores(host.CPU ?? host.cpu)
    case 'mem_used':
      return num(state, 'MemUsed', 'mem_used')
    case 'mem_total':
      return num(host, 'MemTotal', 'mem_total') || num(state, 'MemTotal', 'mem_total')
    case 'swap_used':
      return num(state, 'SwapUsed', 'swap_used')
    case 'swap_total':
      return num(host, 'SwapTotal', 'swap_total') || num(state, 'SwapTotal', 'swap_total')
    case 'disk_used':
      return num(state, 'DiskUsed', 'disk_used')
    case 'disk_total':
      return num(host, 'DiskTotal', 'disk_total') || num(state, 'DiskTotal', 'disk_total')
    default:
      return server.display_index
  }
}

export function resolveServerSortProp(prop: string | undefined | null): ServerSortProp {
  return prop && SORT_PROPS.has(prop as ServerSortProp) ? prop as ServerSortProp : 'display_index'
}

export function sortServers(
  list: readonly ServerRecord[],
  prop: ServerSortProp | string = 'display_index',
  order: ServerSortOrder = 'desc',
): ServerRecord[] {
  const direction = order === 'asc' ? 1 : -1
  const resolved = resolveServerSortProp(prop)
  return [...list].sort((a, b) => {
    const left = sortValue(a, resolved)
    const right = sortValue(b, resolved)
    if (typeof left === 'string' || typeof right === 'string') {
      return String(left).localeCompare(String(right)) * direction
    }
    return (left - right) * direction
  })
}
