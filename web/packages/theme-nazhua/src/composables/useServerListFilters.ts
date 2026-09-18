import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import type { ServerRecord } from '@santaizi/api'
import { isHostOnline } from '@santaizi/api'
import {
  SERVER_SORT_COLUMNS,
  SERVER_SORT_OPTIONS,
  resolveServerSortProp,
  sortServers,
  type ServerSortColumn,
  type ServerSortGroup,
  type ServerSortOption,
  type ServerSortOrder,
  type ServerSortProp,
} from '@santaizi/status-core'
import { resolveServerLocation, count2size } from '../utils/worldMap'

export type ListMode = 'card' | 'server-status'
export type SortProp = ServerSortProp
export type SortOrder = ServerSortOrder
export type SortGroup = ServerSortGroup
export type SortOption = ServerSortOption
export type SortColumn = ServerSortColumn

export const SORT_OPTIONS = SERVER_SORT_OPTIONS
export const SORT_OPTION_COLUMNS = SERVER_SORT_COLUMNS

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
  return sortServers(list, query.sort, query.order)
}

function readStoredSortProp(): SortProp {
  return resolveServerSortProp(localStorage.getItem('santaizi-nazhua-sort-prop'))
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
