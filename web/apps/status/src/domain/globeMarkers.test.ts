import { describe, expect, it } from 'vitest'
import type { ServerRecord } from '@santaizi/api'
import { clusterGlobeMarkers, isGlobeFront } from '@santaizi/theme-server-status'

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

describe('status globe markers', () => {
  it('clusters same-city hosts and counts unlocated ones', () => {
    const { markers, unlocated } = clusterGlobeMarkers([
      server(1, 'HKG-A', { note: { customData: { location: 'HKG' } } }),
      server(2, 'HKG-B', { note: { customData: { location: 'HKG' } }, online: false }),
      server(3, 'GHOST', {}),
    ], 'zh-CN')
    expect(unlocated).toBe(1)
    expect(markers).toHaveLength(1)
    expect(markers[0]?.status).toBe('mixed')
    expect(markers[0]?.ids).toEqual([1, 2])
    expect(markers[0]?.hosts).toEqual([
      { id: 1, name: 'HKG-A', online: true },
      { id: 2, name: 'HKG-B', online: false },
    ])
  })

  it('hides markers on the far hemisphere', () => {
    expect(isGlobeFront(0, 0, 0, 0)).toBe(true)
    expect(isGlobeFront(180, 0, 0, 0)).toBe(false)
  })
})
