import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import type { ServerRecord } from '@santaizi/api'
import { isHostOnline } from '@santaizi/api'
import { parseCpuCores } from '../domain/nazhuaServerView'
import { resolveServerLocation, count2size } from '../utils/worldMap'

export type ListMode = 'card' | 'server-status'
export type SortProp =
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
export type SortOrder = 'asc' | 'desc'
export type SortGroup = 'base' | 'host' | 'net' | 'conn' | 'load' | 'cap'

export interface SortOption {
  prop: SortProp
  labelKey: string
  group: SortGroup
}

export const SORT_OPTIONS: SortOption[] = [
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

const SORT_PROPS = new Set(SORT_OPTIONS.map(option => option.prop))

// 排序菜单按分组分列展示，避免 23 个选项排成一列吃满视口高度
export interface SortColumn {
  group: SortGroup
  options: SortOption[]
}

export const SORT_OPTION_COLUMNS: SortColumn[] = SORT_OPTIONS.reduce<SortColumn[]>((columns, option) => {
  const current = columns[columns.length - 1]
  if (current && current.group === option.group) current.options.push(option)
  else columns.push({ group: option.group, options: [option] })
  return columns
}, [])

export interface ServerListQuery {
  tag: string
  online: 'all' | 'online' | 'offline'
  search: string
  sort: SortProp
  order: SortOrder
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

function sortValue(server: ServerRecord, prop: SortProp): number | string {
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

export function filterAndSortServers(source: ServerRecord[], query: ServerListQuery) {
  const q = query.search.trim().toLowerCase()
  const list = source.filter((server) => {
    if (query.tag && (server.tag || 'default') !== query.tag) return false
    if (query.online === 'online' && !isHostOnline(server)) return false
    if (query.online === 'offline' && isHostOnline(server)) return false
    if (q) {
      const host = asRecord(server.host)
      const hay = [
        server.name,
        server.tag,
        text(host, 'Platform', 'platform'),
        text(host, 'CountryCode', 'country_code'),
      ].join(' ').toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })
  const direction = query.order === 'asc' ? 1 : -1
  const prop = SORT_PROPS.has(query.sort) ? query.sort : 'display_index'
  return [...list].sort((a, b) => {
    const left = sortValue(a, prop)
    const right = sortValue(b, prop)
    if (typeof left === 'string' || typeof right === 'string') {
      return String(left).localeCompare(String(right)) * direction
    }
    return (left - right) * direction
  })
}

function readStoredSortProp(): SortProp {
  const stored = localStorage.getItem('santaizi-nazhua-sort-prop') as SortProp | null
  return stored && SORT_PROPS.has(stored) ? stored : 'display_index'
}

function readStoredSortOrder(): SortOrder {
  return localStorage.getItem('santaizi-nazhua-sort-order') === 'asc' ? 'asc' : 'desc'
}

function readStoredListMode(): ListMode {
  const stored = localStorage.getItem('santaizi-nazhua-list-mode')
  if (stored === 'card' || stored === 'server-status') return stored
  if (stored) localStorage.setItem('santaizi-nazhua-list-mode', 'card')
  return 'card'
}

export function useServerListFilters(servers: MaybeRefOrGetter<ServerRecord[]>) {
  const listMode = ref<ListMode>(readStoredListMode())
  const tagFilter = ref('')
  const onlineFilter = ref<'all' | 'online' | 'offline'>('all')
  const searchWord = ref('')
  const sortProp = ref<SortProp>(readStoredSortProp())
  const sortOrder = ref<SortOrder>(readStoredSortOrder())

  const groups = computed(() => {
    const map = new Map<string, ServerRecord[]>()
    for (const server of toValue(servers)) {
      const name = server.tag || 'default'
      map.set(name, [...(map.get(name) || []), server])
    }
    return [...map.entries()].map(([name, items]) => ({ name, count: items.length }))
  })

  const serverCount = computed(() => {
    const list = toValue(servers)
    return {
      total: list.length,
      online: list.filter(s => isHostOnline(s)).length,
      offline: list.filter(s => !isHostOnline(s)).length,
    }
  })

  const filteredServers = computed(() => {
    return filterAndSortServers(toValue(servers), {
      tag: tagFilter.value,
      online: onlineFilter.value,
      search: searchWord.value,
      sort: sortProp.value,
      order: sortOrder.value,
    })
  })

  const mapLocations = computed(() => {
    const locations: Array<{ key: string; x: number; y: number; size: number; label: string; status: 'online' | 'offline' | 'mixed' }> = []
    const buckets = new Map<string, { x: number; y: number; count: number; labels: string[]; online: number; offline: number }>()
    for (const server of filteredServers.value) {
      const loc = resolveServerLocation(server)
      if (!loc || typeof loc.x !== 'number' || typeof loc.y !== 'number') continue
      const bucket = buckets.get(loc.code) || { x: loc.x, y: loc.y, count: 0, labels: [], online: 0, offline: 0 }
      bucket.count += 1
      bucket.labels.push(server.name)
      if (isHostOnline(server)) bucket.online += 1
      else bucket.offline += 1
      buckets.set(loc.code, bucket)
    }
    buckets.forEach((bucket, key) => {
      locations.push({
        key,
        x: bucket.x,
        y: bucket.y,
        size: count2size(bucket.count),
        label: bucket.labels.join('\n'),
        status: bucket.offline === 0 ? 'online' : bucket.online === 0 ? 'offline' : 'mixed',
      })
    })
    return locations
  })

  function setListMode(mode: ListMode) {
    listMode.value = mode
    localStorage.setItem('santaizi-nazhua-list-mode', mode)
  }

  function setSortProp(prop: SortProp) {
    sortProp.value = prop
    localStorage.setItem('santaizi-nazhua-sort-prop', prop)
  }

  function setSortOrder(order: SortOrder) {
    sortOrder.value = order
    localStorage.setItem('santaizi-nazhua-sort-order', order)
  }

  return {
    listMode,
    tagFilter,
    onlineFilter,
    searchWord,
    sortProp,
    sortOrder,
    groups,
    serverCount,
    filteredServers,
    mapLocations,
    setListMode,
    setSortProp,
    setSortOrder,
  }
}

export function useNavbarStats(servers: MaybeRefOrGetter<ServerRecord[]>) {
  return computed(() => {
    let transferIn = 0
    let transferOut = 0
    let speedIn = 0
    let speedOut = 0
    for (const server of toValue(servers)) {
      if (!isHostOnline(server) || !server.state) continue
      const state = asRecord(server.state)
      transferIn += num(state, 'NetInTransfer', 'net_in_transfer')
      transferOut += num(state, 'NetOutTransfer', 'net_out_transfer')
      speedIn += num(state, 'NetInSpeed', 'net_in_speed')
      speedOut += num(state, 'NetOutSpeed', 'net_out_speed')
    }
    return { transferIn, transferOut, speedIn, speedOut }
  })
}
