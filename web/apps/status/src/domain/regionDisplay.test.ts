import { describe, expect, it } from 'vitest'
import {
  isRegionCode,
  normalizeFlagCode,
  regionDisplayName,
  resolveFlagCode,
  resolveIso2,
  resolveRegionLabel,
} from '@santaizi/theme-server-status'

describe('regionDisplay', () => {
  it('treats IATA and ISO codes as codes, not labels', () => {
    expect(isRegionCode('HKG')).toBe(true)
    expect(isRegionCode('HK')).toBe(true)
    expect(isRegionCode('gb-eng')).toBe(true)
    expect(isRegionCode('香港')).toBe(false)
    expect(isRegionCode('Hong Kong')).toBe(false)
  })

  it('resolves IATA aliases and UK to ISO2', () => {
    expect(resolveIso2('HKG')).toBe('hk')
    expect(resolveIso2('SGP')).toBe('sg')
    expect(resolveIso2('NRT')).toBe('jp')
    expect(resolveIso2('UK')).toBe('gb')
    expect(resolveIso2('hkg')).toBe('hk')
    expect(normalizeFlagCode('UK')).toBe('gb')
    expect(normalizeFlagCode('hkg')).toBe('')
  })

  it('formats short region names and keeps human labels', () => {
    expect(resolveRegionLabel('', 'HKG', 'US', 'zh-CN')).toBe('香港')
    expect(resolveRegionLabel('香港', 'HKG', 'CN', 'zh-CN')).toBe('香港')
    expect(regionDisplayName('us', 'en-US')).toBe('United States')
    expect(resolveFlagCode('cn', 'HKG', 'us')).toBe('cn')
    expect(resolveFlagCode('', 'HKG', 'us')).toBe('hk')
  })
})
