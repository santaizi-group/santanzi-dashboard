import { isHostOnline, type CollectorRecord, type ConnectionPath, type ServerRecord } from '@santaizi/api'
import { DEFAULT_VIEW, locationKey, parseLocation, resolveServerGeo, sphericalMean, type GeoPoint } from '@santaizi/geo'

export type MarkerKind = 'primary' | 'collector' | 'node'
export type MarkerStatus = 'online' | 'offline' | 'mixed'

export interface TopologyMarker {
  id: string
  kind: MarkerKind
  name: string
  lon: number
  lat: number
  derived: boolean
  status: MarkerStatus
  href?: string
  coverage?: string
  rttMs?: number
  count: number
  names: string[]
  /** 与 names 对齐，供同位置多节点分色。 */
  onlines: boolean[]
}

export interface TopologyLink {
  fromId: string
  toId: string
  connected: boolean
  kind: 'path' | 'replication'
  rttMs?: number
}

export interface UnlocatedNode {
  id: string
  name: string
}

export interface TopologyGraph {
  primary: TopologyMarker
  collectors: TopologyMarker[]
  nodes: TopologyMarker[]
  unlocated: UnlocatedNode[]
  /** 有节点落地的国家/地区 ISO2。地球不再点亮板块，仅保留给统计/测试。 */
  countries: string[]
  links: TopologyLink[]
  collectorsOnline: number
  collectorsTotal: number
  pathsConnected: number
  pathsAssigned: number
}

export interface TopologyInput {
  servers: ServerRecord[]
  collectors: CollectorRecord[]
  paths: ConnectionPath[]
  primaryLocation?: string
  siteTitle?: string
}

function collectorStatus(row: CollectorRecord): MarkerStatus {
  if (row.revoked) return 'offline'
  if (row.status === 'online') return 'online'
  if (row.status === 'offline') return 'offline'
  return 'mixed'
}

function pathHandshaked(path: ConnectionPath, collectors: CollectorRecord[]): boolean {
  if (!path.sink.connected) return false
  if (path.observer_kind === 'primary' || path.observer_id === 'primary') return true
  const row = collectors.find(item => item.id === path.observer_id)
  return Boolean(row && collectorStatus(row) === 'online')
}

function serverLocated(server: ServerRecord): { point: GeoPoint; country: string } | null {
  return resolveServerGeo(server)
}

function collectorCovers(collector: CollectorRecord, server: ServerRecord, paths: ConnectionPath[]): boolean {
  if (paths.some(path => path.observer_id === collector.id && path.server_id === server.id)) return true
  const scopes = collector.scopes || []
  if (!scopes.length) return false
  if (scopes.some(scope => scope.type === 'all')) return true
  return scopes.some((scope) => {
    if (scope.type === 'server') return String(scope.value) === String(server.id)
    if (scope.type === 'group' || scope.type === 'tag') return scope.value === server.tag
    return false
  })
}

function mixStatus(online: number, offline: number): MarkerStatus {
  if (online && offline) return 'mixed'
  if (online) return 'online'
  return 'offline'
}

const RING_ANGLES = [0, 45, 90, 135, 180, 225, 270, 315]
/** 约 1° 以内视为同城，走屏幕星座而不是地理错开。 */
const CLUSTER_RAD = Math.PI / 180

/**
 * 推算位置常常互相重合。混种类（主端/从端/节点同城）保持锚点，交给屏幕星座拨开。
 * 仅「同类推算点抢同一空锚」时才 7° 环形错开。手填位置不动。
 */
function spread(
  point: GeoPoint,
  taken: Map<string, Set<MarkerKind>>,
  derived: boolean,
  kind: MarkerKind,
): GeoPoint {
  const record = (candidate: GeoPoint) => {
    const key = locationKey(candidate)
    const kinds = taken.get(key) ?? new Set<MarkerKind>()
    kinds.add(kind)
    taken.set(key, kinds)
    return candidate
  }
  if (!derived) return record(point)
  const occupants = taken.get(locationKey(point))
  if (!occupants || occupants.size === 0) return record(point)
  if ([...occupants].some(item => item !== kind)) return record(point)
  for (const radius of [7, 14, 21]) {
    for (const angle of RING_ANGLES) {
      const rad = angle * Math.PI / 180
      const lat = Math.max(-84, Math.min(84, point.lat + Math.sin(rad) * radius))
      const stretch = Math.max(0.2, Math.cos(lat * Math.PI / 180))
      let lon = point.lon + Math.cos(rad) * radius / stretch
      while (lon > 180) lon -= 360
      while (lon < -180) lon += 360
      const candidate = { lon, lat }
      if (!taken.has(locationKey(candidate))) return record(candidate)
    }
  }
  return record(point)
}

export function buildTopology(input: TopologyInput): TopologyGraph {
  const unlocated: UnlocatedNode[] = []
  const locatedServers: Array<{ server: ServerRecord; point: GeoPoint }> = []
  const countries = new Set<string>()
  for (const server of input.servers) {
    const located = serverLocated(server)
    if (!located) {
      unlocated.push({ id: String(server.id), name: server.name })
      continue
    }
    if (located.country) countries.add(located.country)
    locatedServers.push({ server, point: located.point })
  }

  const nodeGroups = new Map<string, { marker: TopologyMarker; online: number; offline: number }>()
  const serverMarkerId = new Map<number, string>()
  for (const { server, point } of locatedServers) {
    const key = locationKey(point)
    const id = `node:${key}`
    serverMarkerId.set(server.id, id)
    const present = isHostOnline(server)
    const online = present ? 1 : 0
    const offline = present ? 0 : 1
    const existing = nodeGroups.get(id)
    if (!existing) {
      nodeGroups.set(id, {
        online, offline,
        marker: {
          id, kind: 'node', name: server.name, lon: point.lon, lat: point.lat, derived: false,
          status: present ? 'online' : 'offline', href: `/servers?q=${encodeURIComponent(server.name)}`,
          count: 1, names: [server.name], onlines: [present],
        },
      })
      continue
    }
    existing.online += online
    existing.offline += offline
    existing.marker.count += 1
    existing.marker.names.push(server.name)
    existing.marker.onlines.push(present)
    existing.marker.status = mixStatus(existing.online, existing.offline)
    existing.marker.name = existing.marker.count === 2
      ? existing.marker.names.join(' · ')
      : `${existing.marker.names[0]} +${existing.marker.count - 1}`
  }

  const taken = new Map<string, Set<MarkerKind>>()
  for (const id of nodeGroups.keys()) taken.set(id.slice('node:'.length), new Set<MarkerKind>(['node']))
  const locatedPoints = locatedServers.map(item => item.point)
  const parsedPrimary = parseLocation(input.primaryLocation)
  const anchor = parsedPrimary || sphericalMean(locatedPoints) || DEFAULT_VIEW
  const primaryPoint = spread(anchor, taken, !parsedPrimary, 'primary')
  const primary: TopologyMarker = {
    id: 'primary',
    kind: 'primary',
    name: input.siteTitle || 'Santaizi',
    lon: primaryPoint.lon,
    lat: primaryPoint.lat,
    derived: !parsedPrimary,
    status: 'online',
    count: 1,
    names: [input.siteTitle || 'Santaizi'],
    onlines: [true],
  }

  const observers = input.collectors.filter(row => row.kind !== 'probe')
  const collectors: TopologyMarker[] = observers.map((collector) => {
    const parsed = parseLocation(collector.location)
    const covered = locatedServers.filter(item => collectorCovers(collector, item.server, input.paths)).map(item => item.point)
    const point = spread(parsed || sphericalMean(covered) || anchor, taken, !parsed, 'collector')
    const assigned = input.paths.filter(path => path.observer_id === collector.id)
    const connected = assigned.filter(path => pathHandshaked(path, observers))
    const online = collectorStatus(collector) === 'online'
    return {
      id: collector.id,
      kind: 'collector' as const,
      name: collector.name,
      lon: point.lon,
      lat: point.lat,
      derived: !parsed,
      status: collectorStatus(collector),
      href: `/connections?observer_id=${encodeURIComponent(collector.id)}`,
      coverage: `${connected.length}/${assigned.length}`,
      rttMs: online ? collector.heartbeat_rtt_ms : undefined,
      count: 1,
      names: [collector.name],
      onlines: [collectorStatus(collector) === 'online'],
    }
  })

  const links: TopologyLink[] = []
  for (const collector of collectors) {
    const source = input.collectors.find(row => row.id === collector.id)
    links.push({
      fromId: collector.id,
      toId: 'primary',
      connected: collector.status === 'online',
      kind: 'replication',
      rttMs: collector.status === 'online' ? source?.heartbeat_rtt_ms : undefined,
    })
  }
  for (const path of input.paths) {
    const fromId = serverMarkerId.get(path.server_id)
    if (!fromId) continue
    const toId = path.observer_kind === 'primary' ? 'primary' : path.observer_id
    if (toId !== 'primary' && !collectors.some(item => item.id === toId)) continue
    const live = pathHandshaked(path, observers)
    links.push({
      fromId,
      toId,
      connected: live,
      kind: 'path',
      rttMs: live ? path.sink.last_rtt_ms : undefined,
    })
  }

  const assigned = input.paths.length
  const connected = input.paths.filter(path => pathHandshaked(path, observers)).length
  primary.coverage = `${connected}/${assigned}`

  return {
    primary,
    collectors,
    nodes: [...nodeGroups.values()].map(group => group.marker),
    unlocated,
    countries: [...countries],
    links,
    collectorsOnline: observers.filter(row => !row.revoked && row.status === 'online').length,
    collectorsTotal: observers.length,
    pathsConnected: connected,
    pathsAssigned: assigned,
  }
}

export function allMarkers(graph: TopologyGraph): TopologyMarker[] {
  return [graph.primary, ...graph.collectors, ...graph.nodes]
}

export interface LinkViewOptions {
  highlightId?: string
  showRays?: boolean
  showPath?: boolean
  showCollectorPath?: boolean
  showReplication?: boolean
}

export function isCollectorPath(link: TopologyLink): boolean {
  return link.kind === 'path' && link.toId !== 'primary'
}

export function visibleLinks(links: TopologyLink[], options: LinkViewOptions = {}): TopologyLink[] {
  if (options.showRays === false) return []
  const showPath = options.showPath !== false
  const showCollectorPath = options.showCollectorPath !== false
  const showReplication = options.showReplication !== false
  const highlight = options.highlightId || ''
  return links.filter((link) => {
    if (link.kind === 'replication') {
      if (!showReplication) return false
    }
    else if (isCollectorPath(link)) {
      if (!showCollectorPath) return false
    }
    else if (!showPath) {
      return false
    }
    if (highlight && highlight !== 'primary' && link.fromId !== highlight && link.toId !== highlight) return false
    return true
  })
}

export interface ScreenOffset {
  x: number
  y: number
  clustered?: boolean
}

/** 世界地球上 22px ≈ 邻国。同城只允许约 0.7°，钉在城市锚点上。 */
export function siteClusterRadius(globeR: number): number {
  const oneDeg = Math.max(1, globeR) * Math.PI / 180
  return Math.min(6, Math.max(3, oneDeg * 0.7))
}

function closeEnough(a: TopologyMarker, b: TopologyMarker): boolean {
  if (locationKey({ lon: a.lon, lat: a.lat }) === locationKey({ lon: b.lon, lat: b.lat })) return true
  const rad = Math.PI / 180
  const lat1 = a.lat * rad
  const lat2 = b.lat * rad
  const dLat = (b.lat - a.lat) * rad
  const dLon = (b.lon - a.lon) * rad
  const hav = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLon / 2) ** 2
  return 2 * Math.asin(Math.min(1, Math.sqrt(hav))) < CLUSTER_RAD
}

export function colocatedClusters(markers: TopologyMarker[]): TopologyMarker[][] {
  const items = markers.filter(item => Number.isFinite(item.lon) && Number.isFinite(item.lat))
  const parent = items.map((_, index) => index)
  const find = (index: number): number => {
    const current = parent[index] ?? index
    if (current === index) return index
    const root = find(current)
    parent[index] = root
    return root
  }
  for (let i = 0; i < items.length; i++) {
    const left = items[i]
    if (!left) continue
    for (let j = i + 1; j < items.length; j++) {
      const right = items[j]
      if (!right) continue
      if (closeEnough(left, right)) {
        const a = find(i)
        const b = find(j)
        if (a !== b) parent[a] = b
      }
    }
  }
  const groups = new Map<number, TopologyMarker[]>()
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (!item) continue
    const root = find(i)
    const list = groups.get(root)
    if (list) list.push(item)
    else groups.set(root, [item])
  }
  return [...groups.values()]
}

function polar(deg: number, radius: number): ScreenOffset {
  const rad = (deg * Math.PI) / 180
  return { x: Math.cos(rad) * radius, y: -Math.sin(rad) * radius }
}

function fan(
  items: TopologyMarker[],
  centerDeg: number,
  span: number,
  radius: number,
  place: (id: string, offset: ScreenOffset) => void,
) {
  if (!items.length) return
  if (items.length === 1) {
    const only = items[0]
    if (only) place(only.id, polar(centerDeg, radius))
    return
  }
  const start = centerDeg - span / 2
  const step = span / (items.length - 1)
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (item) place(item.id, polar(start + step * i, radius))
  }
}

/** 同城混种类：主端钉在城市点，从端/节点在旁边探出一点。单一种类且只有一个标点不偏移。 */
export function layoutSite(cluster: TopologyMarker[], radius: number): Map<string, ScreenOffset> {
  const result = new Map<string, ScreenOffset>()
  const clustered = cluster.length > 1
  const place = (id: string, offset: ScreenOffset) => {
    result.set(id, clustered ? { ...offset, clustered: true } : offset)
  }
  const zero = { x: 0, y: 0 }
  if (cluster.length <= 1) {
    for (const marker of cluster) place(marker.id, zero)
    return result
  }
  const primary = cluster.filter(item => item.kind === 'primary')
  const collectors = cluster.filter(item => item.kind === 'collector')
  const nodes = cluster.filter(item => item.kind === 'node')
  const kinds = Number(primary.length > 0) + Number(collectors.length > 0) + Number(nodes.length > 0)
  if (kinds <= 1) {
    for (let i = 0; i < cluster.length; i++) {
      const marker = cluster[i]
      if (marker) place(marker.id, polar(-90 + (360 * i) / cluster.length, radius))
    }
    return result
  }
  const hub = primary[0]
  if (hub) place(hub.id, zero)
  if (primary.length && collectors.length && nodes.length) {
    fan(collectors, 210, 40, radius, place)
    fan(nodes, 330, 40, radius, place)
    return result
  }
  if (primary.length && nodes.length) {
    fan(nodes, 270, 40, radius, place)
    return result
  }
  if (primary.length && collectors.length) {
    fan(collectors, 270, 50, radius, place)
    return result
  }
  fan(collectors, 180, 40, radius, place)
  fan(nodes, 0, 40, radius, place)
  return result
}

export function siteOffsets(markers: TopologyMarker[], globeR: number): Map<string, ScreenOffset> {
  const radius = siteClusterRadius(globeR)
  const result = new Map<string, ScreenOffset>()
  for (const cluster of colocatedClusters(markers)) {
    for (const [id, offset] of layoutSite(cluster, radius)) result.set(id, offset)
  }
  return result
}

export interface NodeLatencyRow {
  id: string
  name: string
  online: boolean
  rttMs?: number
}

/** 节点相对主面板的链路延迟。主机离线或主端未连接都算离线。 */
export function primaryLatencyRows(servers: ServerRecord[], paths: ConnectionPath[]): NodeLatencyRow[] {
  const primaryByServer = new Map<number, ConnectionPath>()
  for (const path of paths) {
    if (path.observer_kind !== 'primary') continue
    primaryByServer.set(path.server_id, path)
  }
  return [...servers]
    .sort((a, b) => (a.display_index - b.display_index) || a.id - b.id)
    .map((server) => {
      const path = primaryByServer.get(server.id)
      const online = Boolean(isHostOnline(server) && path?.sink.connected)
      return {
        id: String(server.id),
        name: server.name,
        online,
        rttMs: online ? path?.sink.last_rtt_ms : undefined,
      }
    })
}
