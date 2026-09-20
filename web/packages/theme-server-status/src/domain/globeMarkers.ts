import { isHostOnline, type ServerRecord } from '@santaizi/api'
import {
  canonicalIata,
  cityLabel,
  locationKey,
  publicNoteCustom,
  resolveServerGeo,
} from '@santaizi/geo'

export type GlobeMarkerStatus = 'online' | 'offline' | 'mixed'

export interface GlobeHost {
  id: number
  name: string
  online: boolean
}

export interface GlobeMarker {
  key: string
  lon: number
  lat: number
  hosts: GlobeHost[]
  ids: number[]
  names: string[]
  status: GlobeMarkerStatus
  label: string
}

function mergeStatus(current: GlobeMarkerStatus | undefined, online: boolean): GlobeMarkerStatus {
  if (!current) return online ? 'online' : 'offline'
  if (current === 'mixed') return 'mixed'
  if (current === 'online') return online ? 'online' : 'mixed'
  return online ? 'mixed' : 'offline'
}

function markerLabel(server: ServerRecord, locale: string, fallback: string) {
  const custom = publicNoteCustom(server.public_note)
  const iata = canonicalIata(String(custom.location || '')) || canonicalIata(server.host?.CountryCode)
  if (!iata) return fallback
  const name = cityLabel(iata, locale)
  return name || fallback
}

export function clusterGlobeMarkers(servers: ServerRecord[], locale = 'zh-CN'): {
  markers: GlobeMarker[]
  unlocated: number
} {
  const groups = new Map<string, GlobeMarker>()
  let unlocated = 0
  for (const server of servers) {
    const geo = resolveServerGeo(server)
    if (!geo) {
      unlocated += 1
      continue
    }
    const key = locationKey(geo.point)
    const online = isHostOnline(server)
    const existing = groups.get(key)
    const host = { id: server.id, name: server.name, online }
    if (existing) {
      existing.hosts.push(host)
      existing.ids.push(server.id)
      existing.names.push(server.name)
      existing.status = mergeStatus(existing.status, online)
      continue
    }
    groups.set(key, {
      key,
      lon: geo.point.lon,
      lat: geo.point.lat,
      hosts: [host],
      ids: [server.id],
      names: [server.name],
      status: online ? 'online' : 'offline',
      label: markerLabel(server, locale, server.name),
    })
  }
  return { markers: [...groups.values()], unlocated }
}

/** 与 Admin TopologyGlobe / d3 geoDistance 同：正面半球才画点。clipAngle 不裁 `projection([lon,lat])`。 */
export function isGlobeFront(lon: number, lat: number, rotLambda: number, rotPhi: number, pad = 0.02) {
  const lambda0 = -rotLambda * Math.PI / 180
  const phi0 = -rotPhi * Math.PI / 180
  const lambda1 = lon * Math.PI / 180
  const phi1 = lat * Math.PI / 180
  const cos = Math.sin(phi0) * Math.sin(phi1) + Math.cos(phi0) * Math.cos(phi1) * Math.cos(lambda1 - lambda0)
  return Math.acos(Math.min(1, Math.max(-1, cos))) <= Math.PI / 2 - pad
}
