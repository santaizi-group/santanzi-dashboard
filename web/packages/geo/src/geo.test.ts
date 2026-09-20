import { describe, expect, it } from 'vitest'
import { canonicalIata, cityLabel, DEFAULT_VIEW, formatLocation, locationOptions, parseLocation, resolveServerGeo, sphericalMean } from './geo'
import { IATA_COORDS, IATA_NAMES } from './locationMaps'

describe('parseLocation', () => {
  it('accepts ISO2, aliases, continents and lat,lon', () => {
    expect(parseLocation('CN')?.lon).toBeCloseTo(116.4074)
    expect(parseLocation('cn')?.lat).toBeCloseTo(39.9042)
    expect(parseLocation('UK')?.lon).toBeCloseTo(-0.13, 1)
    expect(parseLocation('EU')).toEqual({ lon: 15, lat: 50 })
    expect(parseLocation('39.9, 116.4')).toEqual({ lon: 116.4, lat: 39.9 })
    expect(parseLocation('')).toBeNull()
    expect(parseLocation('xx')).toBeNull()
    expect(parseLocation('91,0')).toBeNull()
    expect(parseLocation('0,181')).toBeNull()
  })

  it('maps IATA and nazhua aliases like the public map', () => {
    expect(parseLocation('HKG')?.lat).toBeCloseTo(22.3193)
    expect(parseLocation('SGP')?.lon).toBeCloseTo(103.8198)
    expect(parseLocation('SLC')?.lon).toBeCloseTo(-111.891)
    expect(parseLocation('NRT')?.lat).toBeCloseTo(35.6762)
  })

  it('averages points on the sphere', () => {
    const mean = sphericalMean([
      { lon: 116.41, lat: 39.9 },
      { lon: 121.57, lat: 25.03 },
    ])
    expect(mean).toBeTruthy()
    expect(mean!.lon).toBeGreaterThan(116)
    expect(mean!.lon).toBeLessThan(122)
    expect(sphericalMean([])).toBeNull()
    expect(sphericalMean([DEFAULT_VIEW])).toEqual(DEFAULT_VIEW)
  })

  it('formats IATA cities and keeps coordinates', () => {
    expect(formatLocation('LAX', 'zh-CN')).toBe('洛杉矶 · LAX')
    expect(formatLocation('lax', 'en-US')).toBe('Los Angeles · LAX')
    expect(formatLocation('US', 'zh-CN')).toBe('洛杉矶 · LAX')
    expect(formatLocation('NRT', 'zh-CN')).toBe('东京 · TYO')
    expect(formatLocation('35.0, 139.0')).toBe('35.00, 139.00')
  })
})

describe('locationOptions', () => {
  it('lists IATA city codes, not ISO2 countries', () => {
    const zh = locationOptions('zh-CN')
    const en = locationOptions('en-US')
    expect(zh.some(item => item.code === 'LAX' && item.label.includes('洛杉矶'))).toBe(true)
    expect(en.some(item => item.code === 'NYC' && item.label.includes('New York'))).toBe(true)
    expect(zh.some(item => item.code === 'US')).toBe(false)
    expect(canonicalIata('US')).toBe('LAX')
    expect(cityLabel('HKG', 'zh-CN')).toBe('香港')
    expect(Object.keys(IATA_COORDS).every(code => IATA_NAMES[code])).toBe(true)
  })
})

describe('resolveServerGeo', () => {
  it('prefers public-note coordinates, then location code, then CountryCode', () => {
    expect(resolveServerGeo({
      host: { CountryCode: 'US' },
      public_note: { customData: { latlng: '40.7608,-111.891' } },
    })?.point.lon).toBeCloseTo(-111.891)
    expect(resolveServerGeo({
      host: { CountryCode: 'US' },
      public_note: { customData: { location: 'HKG' } },
    })).toMatchObject({ country: 'CN' })
    expect(resolveServerGeo({
      host: { CountryCode: 'US' },
      public_note: { customData: { location: 'HKG' } },
    })?.point.lat).toBeCloseTo(22.3193)
    expect(resolveServerGeo({
      host: { CountryCode: 'JP' },
      public_note: {},
    })?.point.lat).toBeCloseTo(35.6762)
    expect(resolveServerGeo({ host: { CountryCode: 'ZZ' }, public_note: {} })).toBeNull()
  })
})
