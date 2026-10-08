import { describe, expect, it } from 'vitest'
import {
  buildAvailabilitySegments,
  coverageLabel,
  formatDurationMs,
  inferBucketMs,
  summarizeAvailability,
  type AvailabilityBucketLike,
} from './availability'

const evidence = [{ observer_id: 'primary', observer_kind: 'primary', healthy: true, seen: true }]

function bucket(start: string, extra: Partial<AvailabilityBucketLike> = {}): AvailabilityBucketLike {
  return {
    bucket_start: start,
    host: 'online',
    connectivity: 'full',
    expected_observers: 2,
    healthy_observers: 2,
    seen_observers: 2,
    observer_evidence: evidence,
    ...extra,
  }
}

describe('availability segments', () => {
  it('infers bucket length from adjacent starts and falls back to 30s', () => {
    expect(inferBucketMs([])).toBe(30_000)
    expect(inferBucketMs([bucket('2026-08-13T06:00:00Z')])).toBe(30_000)
    expect(inferBucketMs([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z'),
      bucket('2026-08-13T06:01:30Z'),
    ])).toBe(30_000)
    expect(inferBucketMs([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:01:00Z'),
    ])).toBe(60_000)
  })

  it('uses window_end when mixed resolutions are present', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T04:00:00Z', { window_end: '2026-08-13T05:00:00Z' }),
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z'),
    ])
    expect((segments[0]?.end ?? 0) - (segments[0]?.start ?? 0)).toBe(3_600_000)
    expect(segments.map(item => item.kind)).toEqual(['observed', 'gap', 'observed'])
  })

  it('merges consecutive same-state buckets', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z'),
      bucket('2026-08-13T06:01:00Z'),
    ])
    expect(segments).toHaveLength(1)
    const merged = segments[0]
    expect(merged?.kind).toBe('observed')
    expect((merged?.end ?? 0) - (merged?.start ?? 0)).toBe(90_000)
    expect(merged ? coverageLabel(merged) : '').toBe('2/2')
  })

  it('splits when host or connectivity changes', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z', { host: 'offline', connectivity: 'unavailable' }),
    ])
    expect(segments.map(item => item.connectivity)).toEqual(['full', 'unavailable'])
    expect(segments.every(item => item.kind === 'observed')).toBe(true)
  })

  it('inserts a gap between non-adjacent buckets', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z'),
      bucket('2026-08-13T06:01:00Z'),
      bucket('2026-08-13T06:02:00Z'),
    ])
    expect(segments.map(item => item.kind)).toEqual(['observed', 'gap', 'observed'])
    const gap = segments[1]
    expect((gap?.end ?? 0) - (gap?.start ?? 0)).toBe(30_000)
    expect(gap ? coverageLabel(gap) : '').toBe('—')
  })

  it('returns an empty list for no rows and a single bucket as one segment', () => {
    expect(buildAvailabilitySegments([])).toEqual([])
    const segments = buildAvailabilitySegments([bucket('2026-08-13T06:00:00Z')])
    expect(segments).toHaveLength(1)
    expect((segments[0]?.end ?? 0) - (segments[0]?.start ?? 0)).toBe(30_000)
  })

  it('excludes unknown and gap time from availability percent', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z', { connectivity: 'unknown' }),
      bucket('2026-08-13T06:01:00Z', { host: 'offline', connectivity: 'unavailable' }),
      bucket('2026-08-13T06:02:00Z'),
    ])
    const summary = summarizeAvailability(segments)
    expect(summary.availableMs).toBe(60_000)
    expect(summary.partialMs).toBe(0)
    expect(summary.unavailableMs).toBe(30_000)
    expect(summary.unknownMs).toBe(30_000)
    expect(summary.gapMs).toBe(30_000)
    expect(summary.availablePercent).toBeCloseTo(200 / 3)
    expect(summary.outageCount).toBe(1)
    expect(summary.degradedCount).toBe(0)
  })

  it('counts partial connectivity as uptime and one degradation', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z'),
      bucket('2026-08-13T06:00:30Z', { connectivity: 'partial', seen_observers: 1, observer_evidence: [{ observer_id: 'primary', seen: true, healthy: true }] }),
    ])
    const summary = summarizeAvailability(segments)
    expect(summary.availableMs).toBe(30_000)
    expect(summary.partialMs).toBe(30_000)
    expect(summary.availablePercent).toBe(100)
    expect(summary.outageCount).toBe(0)
    expect(summary.degradedCount).toBe(1)
  })

  it('keeps a continuously seen host at full availability when coverage is partial', () => {
    const fullMs = 47_910_000
    const partialMs = 26_220_000
    const start = Date.parse('2026-10-07T01:54:00.000Z')
    const segments = buildAvailabilitySegments([
      bucket(new Date(start).toISOString(), {
        window_end: new Date(start + partialMs).toISOString(),
        connectivity: 'partial',
        seen_observers: 2,
        expected_observers: 3,
        healthy_observers: 3,
      }),
      bucket(new Date(start + partialMs).toISOString(), {
        window_end: new Date(start + partialMs + fullMs).toISOString(),
      }),
    ])
    const summary = summarizeAvailability(segments)
    expect(summary.availableMs).toBe(fullMs)
    expect(summary.partialMs).toBe(partialMs)
    expect(summary.unavailableMs).toBe(0)
    expect(summary.availablePercent).toBeCloseTo(100)
    expect(summary.outageCount).toBe(0)
    expect(summary.degradedCount).toBe(1)
  })

  it('counts consecutive partial segments as one degradation', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z', { connectivity: 'partial', seen_observers: 1, observer_evidence: [{ observer_id: 'primary', seen: true, healthy: true }] }),
      bucket('2026-08-13T06:00:30Z', { connectivity: 'partial', seen_observers: 1, observer_evidence: [{ observer_id: 'collector-a', seen: true, healthy: true }] }),
    ])
    expect(segments).toHaveLength(2)
    expect(summarizeAvailability(segments).degradedCount).toBe(1)
  })

  it('counts consecutive unavailable segments as one outage', () => {
    const segments = buildAvailabilitySegments([
      bucket('2026-08-13T06:00:00Z', { connectivity: 'unavailable', seen_observers: 1, observer_evidence: [{ observer_id: 'primary', seen: true, healthy: true }] }),
      bucket('2026-08-13T06:00:30Z', { connectivity: 'unavailable', seen_observers: 0, observer_evidence: [{ observer_id: 'primary', seen: false, healthy: true }] }),
    ])
    expect(segments).toHaveLength(2)
    expect(summarizeAvailability(segments).outageCount).toBe(1)
  })

  it('formats compact durations', () => {
    expect(formatDurationMs(0)).toBe('0s')
    expect(formatDurationMs(45_000)).toBe('45s')
    expect(formatDurationMs(90_000)).toBe('1m 30s')
    expect(formatDurationMs(3_600_000)).toBe('1h')
    expect(formatDurationMs(5_400_000)).toBe('1h 30m')
  })
})
