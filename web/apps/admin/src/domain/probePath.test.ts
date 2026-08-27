import { describe, expect, it } from 'vitest'
import {
  normalizeProbeHops,
  hopGeoLine,
  formatHopASN,
  probeHopLossTone,
  probeHopRouteStats,
  probeHopRttShare,
  probeRouteChanged,
} from './probePath'

describe('normalizeProbeHops', () => {
  it('maps MTR avg_ms and nexttrace rtt_ms onto rttMs', () => {
    expect(normalizeProbeHops([
      { ttl: 1, address: '10.0.0.1', avg_ms: 1.2, private: true, loss: 0 },
      { ttl: 2, address: ' 192.0.2.1 ', hostname: ' edge.example ', rtt_ms: 80, geo: 'United States', asn: '4134', loss: 0.2 },
    ])).toEqual([
      { ttl: 1, address: '10.0.0.1', hostname: '', geo: '', asn: '', private: true, rttMs: 1.2, loss: 0 },
      { ttl: 2, address: '192.0.2.1', hostname: 'edge.example', geo: 'United States', asn: '4134', private: false, rttMs: 80, loss: 0.2 },
    ])
  })

  it('prefers rtt_ms when both latency fields exist and drops invalid RTT', () => {
    expect(normalizeProbeHops([
      { ttl: 1, rtt_ms: 12, avg_ms: 99 },
      { ttl: 2, avg_ms: -1 },
      { ttl: 3 },
    ]).map(hop => hop.rttMs)).toEqual([12, undefined, undefined])
  })

  it('treats empty input as no hops', () => {
    expect(normalizeProbeHops()).toEqual([])
    expect(normalizeProbeHops(null)).toEqual([])
  })
})

describe('probeHopRouteStats', () => {
  it('tracks max RTT and treats last addressed hop as reached', () => {
    const hops = normalizeProbeHops([
      { ttl: 1, address: '10.0.0.1', avg_ms: 2, loss: 0 },
      { ttl: 2, address: '218.89.1.1', avg_ms: 9, loss: 0 },
      { ttl: 3 },
      { ttl: 4, address: '192.0.2.88', avg_ms: 171, loss: 0 },
    ])
    expect(probeHopRouteStats(hops)).toEqual({
      hopCount: 4,
      maxRttMs: 171,
      lastIndex: 3,
      lastRttMs: 171,
      lastLoss: 0,
      reached: true,
    })
  })

  it('treats a timed-out last hop as unreached', () => {
    const hops = normalizeProbeHops([
      { ttl: 1, address: '10.0.0.1', avg_ms: 3, loss: 0 },
      { ttl: 2, address: '10.0.0.2', avg_ms: 5, loss: 0 },
      { ttl: 3, loss: 1 },
    ])
    expect(probeHopRouteStats(hops)).toMatchObject({
      hopCount: 3,
      lastIndex: 2,
      reached: false,
      lastLoss: 1,
    })
  })
})

describe('probeHopRttShare', () => {
  it('scales against the max hop RTT', () => {
    expect(probeHopRttShare(85, 170)).toBe(50)
    expect(probeHopRttShare(undefined, 170)).toBe(0)
    expect(probeHopRttShare(10, 0)).toBe(0)
  })
})

describe('probeHopLossTone', () => {
  it('grades missing, zero, mid and high loss', () => {
    expect(probeHopLossTone(undefined)).toBe('')
    expect(probeHopLossTone(0)).toBe('is-ok')
    expect(probeHopLossTone(0.2)).toBe('is-warn')
    expect(probeHopLossTone(0.5)).toBe('is-fail')
    expect(probeHopLossTone(80)).toBe('is-fail')
  })
})

describe('probeRouteChanged', () => {
  it('compares address sequences and ignores missing previous', () => {
    const current = [{ address: '10.0.0.1' }, { address: '192.0.2.1' }]
    expect(probeRouteChanged(current, [{ address: '10.0.0.1' }, { address: '192.0.2.1' }])).toBe(false)
    expect(probeRouteChanged(current, [{ address: '10.0.0.1' }, { address: '' }])).toBe(true)
    expect(probeRouteChanged(current, [{ address: '10.0.0.1' }])).toBe(true)
    expect(probeRouteChanged(current, [])).toBe(false)
    expect(probeRouteChanged([], current)).toBe(false)
  })
})

describe('hopGeoLine', () => {
  it('appends ASN when geo does not already include it', () => {
    expect(hopGeoLine({ geo: '中国 · 上海 · 电信', asn: '4809' }, '内网')).toBe('中国 · 上海 · 电信 · AS4809')
    expect(hopGeoLine({ geo: '中国 · AS4134', asn: '4134' }, '内网')).toBe('中国 · AS4134')
    expect(hopGeoLine({ private: true, asn: '4134' }, '内网')).toBe('内网')
    expect(formatHopASN('as4134')).toBe('AS4134')
  })
})
