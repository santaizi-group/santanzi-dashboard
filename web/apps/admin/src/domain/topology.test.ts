import { describe, expect, it } from 'vitest'
import type { CollectorRecord, ServerRecord } from '@santaizi/api'
import type { ConnectionPath } from '@santaizi/api'
import { DEFAULT_VIEW } from '@santaizi/geo'
import { allMarkers, buildTopology, layoutSite, primaryLatencyRows, siteClusterRadius, siteOffsets, visibleLinks } from './topology'

function server(id: number, name: string, extra: { country?: string; note?: Record<string, unknown>; online?: boolean; telemetry?: ServerRecord['telemetry'] } = {}): ServerRecord {
  return {
    id, name, tag: 'edge', display_index: id, hide_for_guest: false, enable_ddns: false, online: extra.online ?? true,
    host: extra.country ? { CountryCode: extra.country } : undefined,
    public_note: extra.note,
    telemetry: extra.telemetry,
  }
}

function collector(id: string, name: string, extra: Partial<CollectorRecord> = {}): CollectorRecord {
  return {
    id, name, address: `${id}.example:5555`, tls: true, insecure_tls: false,
    generation: 1, config_version: 1, revoked: false, status: 'online',
    scopes: [{ type: 'all', value: '' }],
    ...extra,
  }
}

function path(serverId: number, observerId: string, kind: 'primary' | 'collector', connected = true): ConnectionPath {
  return {
    server_id: serverId, server_name: `s${serverId}`, node_uuid: 'aa', observer_id: observerId,
    observer_kind: kind, observer_name: observerId === 'primary' ? '' : 'edge', assigned: true,
    sink: { connected, last_rtt_ms: connected ? 12 : undefined },
    display_index: serverId,
  }
}

describe('buildTopology', () => {
  it('places nodes by CountryCode and aggregates the same location', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'CN' }), server(2, 'b', { country: 'CN' }), server(3, 'c')],
      collectors: [],
      paths: [],
      siteTitle: '三太子监控',
    })
    expect(graph.nodes).toHaveLength(1)
    expect(graph.nodes[0]?.count).toBe(2)
    expect(graph.nodes[0]?.onlines).toEqual([true, true])
    expect(graph.unlocated).toEqual([{ id: '3', name: 'c' }])
    expect(graph.countries).toEqual(['CN'])
    expect(graph.primary.derived).toBe(true)
    expect(graph.primary.lon).toBeCloseTo(116.41)
    expect(graph.primary.lat).toBeCloseTo(39.9)
  })

  it('derives collector position from covered nodes, then falls back to primary', () => {
    const derived = buildTopology({
      servers: [server(1, 'tokyo', { country: 'JP' })],
      collectors: [collector('c1', 'edge')],
      paths: [path(1, 'c1', 'collector')],
      primaryLocation: 'DE',
    })
    expect(derived.collectors[0]?.derived).toBe(true)
    expect(derived.collectors[0]?.lon).toBeCloseTo(139.65)
    expect(derived.collectors[0]?.lat).toBeCloseTo(35.68)

    const empty = buildTopology({
      servers: [],
      collectors: [collector('c1', 'edge')],
      paths: [],
      primaryLocation: 'DE',
    })
    expect(Math.abs((empty.collectors[0]?.lon ?? 0) - 8.68)).toBeLessThan(15)
    expect(empty.primary.derived).toBe(false)
    expect(empty.primary.lon).toBeCloseTo(8.68)
  })

  it('keeps mixed-kind markers on the same city for screen clustering', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'CN' })],
      collectors: [collector('c1', 'one'), collector('c2', 'two')],
      paths: [],
    })
    const node = graph.nodes[0]
    expect(node?.lon).toBeCloseTo(116.41)
    expect(graph.primary.lon).toBeCloseTo(116.41)
    expect(graph.collectors[0]?.lon).toBeCloseTo(116.41)
    expect(graph.collectors[1]?.lon).toBeCloseTo(116.41)
    const offsets = siteOffsets(allMarkers(graph), 200)
    const keys = [graph.primary, ...graph.collectors, ...graph.nodes].map(marker => {
      const offset = offsets.get(marker.id)
      return `${offset?.x.toFixed(1)},${offset?.y.toFixed(1)}`
    })
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('still spreads derived collectors that collide without mixed kinds', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'CN' }), server(2, 'b', { country: 'JP' })],
      collectors: [collector('c1', 'one'), collector('c2', 'two')],
      paths: [],
      primaryLocation: 'DE',
    })
    expect(graph.collectors[0]?.lon).not.toBeCloseTo(graph.collectors[1]?.lon ?? 0)
  })

  it('uses the default facing when nothing is located', () => {
    const graph = buildTopology({ servers: [server(1, 'ghost')], collectors: [], paths: [] })
    expect(graph.primary.lon).toBe(DEFAULT_VIEW.lon)
    expect(graph.primary.lat).toBe(DEFAULT_VIEW.lat)
    expect(graph.links).toEqual([])
  })

  it('does not count path RTT when the collector is offline', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'SG' })],
      collectors: [collector('c1', 'sin', { status: 'offline', heartbeat_rtt_ms: 18 })],
      paths: [path(1, 'c1', 'collector', true)],
      primaryLocation: 'CN',
    })
    const link = graph.links.find(item => item.kind === 'path' && item.toId === 'c1')
    expect(link).toMatchObject({ connected: false, rttMs: undefined })
    expect(graph.pathsConnected).toBe(0)
    expect(graph.collectors[0]?.rttMs).toBeUndefined()
  })

  it('builds path and replication links', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'SG' })],
      collectors: [collector('c1', 'sin')],
      paths: [path(1, 'primary', 'primary', true), path(1, 'c1', 'collector', false)],
      primaryLocation: 'CN',
    })
    expect(graph.links.some(link => link.kind === 'replication' && link.toId === 'primary')).toBe(true)
    expect(graph.links.some(link => link.kind === 'path' && link.toId === 'c1' && !link.connected)).toBe(true)
    expect(graph.pathsAssigned).toBe(2)
    expect(graph.pathsConnected).toBe(1)
    expect(graph.collectors[0]?.coverage).toBe('0/1')
  })

  it('keeps per-node online flags when the same location mixes status', () => {
    const graph = buildTopology({
      servers: [
        server(1, 'up', { country: 'CN', online: true }),
        server(2, 'down', { country: 'CN', online: false }),
      ],
      collectors: [],
      paths: [],
    })
    expect(graph.nodes[0]?.count).toBe(2)
    expect(graph.nodes[0]?.status).toBe('mixed')
    expect(graph.nodes[0]?.onlines).toEqual([true, false])
  })

  it('places nodes from public-note location before CountryCode', () => {
    const graph = buildTopology({
      servers: [server(1, 'salt', { country: 'US', note: { customData: { location: 'SLC' } } })],
      collectors: [],
      paths: [],
    })
    expect(graph.nodes[0]?.lon).toBeCloseTo(-111.891)
    expect(graph.unlocated).toEqual([])
    expect(graph.countries).toEqual(['US'])
  })

  it('lists primary latency and marks offline nodes', () => {
    const rows = primaryLatencyRows(
      [server(2, 'ghost', { online: false }), server(1, 'tokyo')],
      [path(1, 'primary', 'primary', true), path(1, 'c1', 'collector', true)],
    )
    expect(rows.map(row => row.name)).toEqual(['tokyo', 'ghost'])
    expect(rows[0]).toMatchObject({ online: true, rttMs: 12 })
    expect(rows[1]).toMatchObject({ online: false, rttMs: undefined })
  })

  it('treats a disconnected primary path as offline', () => {
    const rows = primaryLatencyRows(
      [server(1, 'tokyo')],
      [path(1, 'primary', 'primary', false)],
    )
    expect(rows[0]).toMatchObject({ online: false, rttMs: undefined })
  })

  it('keeps a host online when observers still see it despite a stale LastActive flag', () => {
    const rows = primaryLatencyRows(
      [server(1, 'tokyo', { online: false, telemetry: { host: 'online', connectivity: 'partial', available: true, coverage: '1/2' } })],
      [path(1, 'primary', 'primary', true)],
    )
    expect(rows[0]).toMatchObject({ online: true, rttMs: 12 })
  })

  it('keeps hand-filled primary, collector and node on the same city', () => {
    const graph = buildTopology({
      servers: [server(1, 'sg', { note: { customData: { location: 'SGP' } } })],
      collectors: [collector('c1', 'edge', { location: 'SGP' })],
      paths: [path(1, 'primary', 'primary'), path(1, 'c1', 'collector')],
      primaryLocation: 'SGP',
    })
    const node = graph.nodes[0]
    expect(node).toBeTruthy()
    expect(graph.primary.lon).toBeCloseTo(node!.lon)
    expect(graph.primary.lat).toBeCloseTo(node!.lat)
    expect(graph.collectors[0]?.lon).toBeCloseTo(node!.lon)
    const laid = layoutSite(allMarkers(graph), 6)
    expect(laid.get('primary')).toMatchObject({ x: 0, y: 0 })
    expect(laid.get('c1')?.x).toBeLessThan(0)
    expect(laid.get(node!.id)?.x).toBeGreaterThan(0)
    const globeR = 200
    const offsets = siteOffsets(allMarkers(graph), globeR)
    const limit = siteClusterRadius(globeR) + 0.01
    for (const offset of offsets.values()) {
      expect(Math.hypot(offset.x, offset.y)).toBeLessThanOrEqual(limit)
    }
  })
})

describe('visibleLinks', () => {
  const links = [
    { fromId: 'node:a', toId: 'primary', connected: true, kind: 'path' as const },
    { fromId: 'node:a', toId: 'c1', connected: true, kind: 'path' as const },
    { fromId: 'c1', toId: 'primary', connected: true, kind: 'replication' as const },
  ]

  it('hides all rays', () => {
    expect(visibleLinks(links, { showRays: false })).toEqual([])
  })

  it('hides a connection kind', () => {
    expect(visibleLinks(links, { showPath: false }).map(item => `${item.kind}:${item.toId}`)).toEqual(['path:c1', 'replication:primary'])
    expect(visibleLinks(links, { showCollectorPath: false }).map(item => `${item.kind}:${item.toId}`)).toEqual(['path:primary', 'replication:primary'])
    expect(visibleLinks(links, { showReplication: false }).map(item => item.kind)).toEqual(['path', 'path'])
  })

  it('keeps links that touch the highlighted node', () => {
    expect(visibleLinks(links, { highlightId: 'node:a' }).map(item => item.toId)).toEqual(['primary', 'c1'])
    expect(visibleLinks(links, { highlightId: 'primary' })).toEqual(links)
  })
})

describe('layoutSite', () => {
  function marker(id: string, kind: 'primary' | 'collector' | 'node'): ReturnType<typeof allMarkers>[number] {
    return {
      id, kind, name: id, lon: 0, lat: 0, derived: false, status: 'online',
      count: 1, names: [id], onlines: [true],
    }
  }

  it('does not offset a single marker', () => {
    const laid = layoutSite([marker('primary', 'primary')], 26)
    expect(laid.get('primary')).toEqual({ x: 0, y: 0 })
  })

  it('pins primary at the city and peeks collector/node beside it', () => {
    const laid = layoutSite([
      marker('primary', 'primary'),
      marker('c1', 'collector'),
      marker('n1', 'node'),
    ], 6)
    expect(laid.get('primary')).toMatchObject({ x: 0, y: 0 })
    expect(laid.get('c1')?.x).toBeLessThan(0)
    expect(laid.get('n1')?.x).toBeGreaterThan(0)
    expect(Math.hypot(laid.get('c1')!.x, laid.get('c1')!.y)).toBeCloseTo(6)
  })

  it('caps world-globe cluster radius under one degree', () => {
    expect(siteClusterRadius(200)).toBeLessThanOrEqual(200 * Math.PI / 180)
  })
})

describe('probe collectors', () => {
  it('omits probe collectors from globe markers and counts', () => {
    const graph = buildTopology({
      servers: [server(1, 'a', { country: 'SG' })],
      collectors: [collector('c1', 'obs'), collector('p1', 'probe', { kind: 'probe' })],
      paths: [path(1, 'c1', 'collector')],
    })
    expect(graph.collectors.map(item => item.id)).toEqual(['c1'])
    expect(graph.collectorsTotal).toBe(1)
    expect(graph.links.some(link => link.fromId === 'p1' || link.toId === 'p1')).toBe(false)
  })
})
