import { describe, expect, it } from 'vitest'
import {
  formatAvailabilityPercent,
  formatCycleBytes,
  formatProductVersion,
  formatTransfer,
  getCycleTransferStatusLevel,
  osLabel,
  osLogoClass,
  pickCycleSummary,
  trafficUsage,
  truncateDecimal,
  usageTone,
} from '@santaizi/theme-server-status'

describe('formatTransfer', () => {
  it('uses aobobo short units and >1 thresholds', () => {
    expect(formatTransfer(0)).toBe('0K')
    expect(formatTransfer(512)).toBe('0.5K')
    expect(formatTransfer(1024)).toBe('1K')
    expect(formatTransfer(1024 ** 2)).toBe('1024K')
    expect(formatTransfer(1024 ** 2 + 1)).toBe('1M')
    expect(formatTransfer(2 * 1024 ** 3)).toBe('2G')
    expect(formatTransfer(1.5 * 1024 ** 4)).toBe('1.5T')
  })
})

describe('formatCycleBytes', () => {
  it('matches nazhua-front adaptive decimals and long units', () => {
    expect(formatCycleBytes(0)).toBe('0B')
    expect(formatCycleBytes(512)).toBe('512B')
    expect(formatCycleBytes(1024)).toBe('1KB')
    expect(formatCycleBytes(10 * 1024 ** 2)).toBe('10MB')
    expect(formatCycleBytes(925_512_590_787)).toBe('862GB')
  })
})

describe('trafficUsage', () => {
  it('prefers cycle remaining over raw totals', () => {
    const view = trafficUsage(10, 20, '2', { remainingLabel: '861.95G', statusLevel: 'fine' })
    expect(view).toEqual({ kind: 'cycle', valueLabel: '861.95G', statusLevel: 'fine' })
  })

  it('aggregates by trafficType', () => {
    expect(trafficUsage(100, 200, '1').kind).toBe('out')
    expect(trafficUsage(100, 200, '1').valueLabel).toBe(formatTransfer(200))
    expect(trafficUsage(100, 200, '2').kind).toBe('both')
    expect(trafficUsage(100, 200, '2').valueLabel).toBe(formatTransfer(300))
    expect(trafficUsage(500, 200, '3').kind).toBe('maxIn')
    expect(trafficUsage(100, 200, '3').kind).toBe('maxOut')
    expect(trafficUsage(100, 200, 1 as unknown as string).kind).toBe('out')
    expect(trafficUsage(1, 1, '').kind).toBe('unlimited')
  })
})

describe('cycle remaining status', () => {
  it('matches aobobo remaining-percent bands', () => {
    expect(getCycleTransferStatusLevel(null)).toBe('neutral')
    expect(getCycleTransferStatusLevel(80)).toBe('fine')
    expect(getCycleTransferStatusLevel(21)).toBe('warning')
    expect(getCycleTransferStatusLevel(10)).toBe('alert')
    expect(getCycleTransferStatusLevel(0)).toBe('over')
  })

  it('picks the most severe cycle first', () => {
    const picked = pickCycleSummary([
      { statusLevel: 'fine' as const, policyId: 1 },
      { statusLevel: 'alert' as const, policyId: 2 },
      { statusLevel: 'warning' as const, policyId: 3 },
    ])
    expect(picked?.policyId).toBe(2)
  })
})

describe('availability percent', () => {
  it('truncates instead of rounding', () => {
    expect(truncateDecimal(99.999, 2)).toBe(99.99)
    expect(formatAvailabilityPercent(99.999)).toBe('99.99%')
    expect(formatAvailabilityPercent(100)).toBe('100.00%')
  })
})

describe('usageTone / os / version', () => {
  it('bands usage colors', () => {
    expect(usageTone(10)).toBe('ok')
    expect(usageTone(70)).toBe('warn')
    expect(usageTone(90)).toBe('danger')
  })

  it('maps OS logos per ADR-0011', () => {
    expect(osLogoClass('debian')).toBe('fl-debian')
    expect(osLogoClass('Windows')).toBe('ri-microsoft-fill')
    expect(osLogoClass('unknown-os')).toBe('ri-computer-line')
    expect(osLabel('debian')).toBe('Debian')
    expect(osLabel('centos')).toBe('CentOS')
    expect(osLabel('opensuse')).toBe('openSUSE')
    expect(osLabel('rocky')).toBe('Rocky')
    expect(osLabel('linux mint')).toBe('Linux Mint')
    expect(osLabel('')).toBe('')
  })

  it('normalizes product versions without doubling v', () => {
    expect(formatProductVersion('v1.0.2')).toBe('v1.0.2')
    expect(formatProductVersion('1.0.2')).toBe('v1.0.2')
    expect(formatProductVersion('test')).toBe('test')
    expect(formatProductVersion('debug')).toBe('debug')
  })
})
