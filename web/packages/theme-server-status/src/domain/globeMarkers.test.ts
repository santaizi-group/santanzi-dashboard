import { describe, expect, it } from 'vitest'
import type { ServerRecord } from '@santaizi/api'
import { clusterGlobeMarkers, isGlobeFront } from './globeMarkers'

function server(id: number, name: string, extra: {
  country?: string
  note?: Record<string, unknown>
  online?: boolean
} = {}): ServerRecord {
  return {
    id,
    name,
    tag: 'edge',
    display_index: id,
    hide_for_guest: false,
    enable_ddns: false,
    online: extra.online ?? true,
    host: extra.country ? { CountryCode: extra.country } : undefined,
    public_note: extra.note,
  }
}

describe('clusterGlobeMarkers', () => {
  it('merges the same city and counts unlocated hosts', () => {
    const { markers, unlocated } = clusterGlobeMarkers([
      server(1, 'HKG-A', { note: { customData: { location: 'HKG' } } }),
      server(2, 'HKG-B', { note: { customData: { location: 'HKG' } }, online: false }),
      server(3, 'SGP', { country: 'SG' }),
      server(4, 'NOWHERE', { country: 'ZZ' }),
    ], 'zh-CN')
    expect(unlocated).toBe(1)
    expect(markers).toHaveLength(2)
    const hongKong = markers.find(item => item.names.includes('HKG-A'))
    expect(hongKong?.status).toBe('mixed')
    expect(hongKong?.ids).toEqual([1, 2])
    expect(hongKong?.hosts).toEqual([
      { id: 1, name: 'HKG-A', online: true },
      { id: 2, name: 'HKG-B', online: false },
    ])
    expect(hongKong?.label).toBe('香港')
    expect(markers.find(item => item.names.includes('SGP'))?.status).toBe('online')
  })
})

describe('isGlobeFront', () => {
  it('hides the antipode and keeps the facing hemisphere', () => {
    expect(isGlobeFront(0, 0, 0, 0)).toBe(true)
    expect(isGlobeFront(180, 0, 0, 0)).toBe(false)
    expect(isGlobeFront(0, 0, 180, 0)).toBe(false)
    expect(isGlobeFront(114.2, 22.3, -114.2, -22.3)).toBe(true)
  })
})
