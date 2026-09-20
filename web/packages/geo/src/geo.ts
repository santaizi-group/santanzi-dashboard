import { COUNTRY_IATA, IATA_COORDS, IATA_NAMES, LOCATION_ALIAS, REGION_COORDS } from './locationMaps'

export interface GeoPoint {
  lon: number
  lat: number
}

/** 无任何节点可推算时，地球默认朝向。 */
export const DEFAULT_VIEW: GeoPoint = { lon: 0, lat: 20 }

const CONTINENTS = new Set(['AN', 'EU', 'OC'])

/** lon, lat。含 ISO2 与不冲突的大洲码（EU/OC/AN）。 */
const COORDS: Record<string, readonly [number, number]> = {
  AN: [0, -80], EU: [15, 50], OC: [145, -20],
  AD: [1.52, 42.51], AE: [55.27, 25.2], AF: [69.17, 34.53], AL: [19.82, 41.33], AM: [44.51, 40.18], AR: [-58.38, -34.6],
  AT: [16.37, 48.21], AU: [151.21, -33.87], AZ: [49.87, 40.41], BA: [18.41, 43.86], BD: [90.41, 23.81],
  BE: [4.35, 50.85], BG: [23.32, 42.7], BH: [50.59, 26.23], BM: [-64.75, 32.31], BN: [114.94, 4.9],
  BO: [-68.15, -16.5], BR: [-46.63, -23.55], BY: [27.57, 53.9], CA: [-79.38, 43.65], CH: [8.54, 47.38],
  CL: [-70.67, -33.45], CN: [116.41, 39.9], CO: [-74.07, 4.71], CR: [-84.09, 9.93], CY: [33.38, 35.19],
  CZ: [14.44, 50.08], DE: [8.68, 50.11], DK: [12.57, 55.68], DZ: [3.06, 36.75], EC: [-78.47, -0.18],
  EE: [24.75, 59.44], EG: [31.24, 30.04], ES: [-3.7, 40.42], FI: [24.94, 60.17], FJ: [178.44, -18.14],
  FO: [-6.77, 62.01], FR: [2.35, 48.86], GB: [-0.13, 51.51], GE: [44.83, 41.72], GG: [-2.59, 49.47],
  GI: [-5.35, 36.14], GR: [23.73, 37.98], HK: [114.17, 22.32], HR: [15.98, 45.81], HU: [19.04, 47.5],
  ID: [106.85, -6.21], IE: [-6.26, 53.35], IL: [34.78, 32.09], IM: [-4.55, 54.24], IN: [77.21, 28.61],
  IQ: [44.37, 33.32], IR: [51.39, 35.69], IS: [-21.83, 64.15], IT: [9.19, 45.46], JE: [-2.13, 49.21],
  JO: [35.91, 31.95], JP: [139.65, 35.68], KE: [36.82, -1.29], KG: [74.59, 42.87], KH: [104.92, 11.56],
  KR: [126.98, 37.57], KW: [47.98, 29.38], KZ: [76.93, 43.24], LA: [102.63, 17.98], LB: [35.5, 33.89],
  LI: [9.52, 47.14], LK: [79.86, 6.93], LT: [25.28, 54.69], LU: [6.13, 49.61], LV: [24.11, 56.95],
  MA: [-6.85, 34.02], MC: [7.42, 43.74], MD: [28.86, 47.01], ME: [19.26, 42.43], MK: [21.43, 41.1],
  MM: [96.16, 16.84], MN: [106.91, 47.92], MO: [113.54, 22.2], MT: [14.51, 35.9], MU: [57.5, -20.16],
  MV: [73.51, 4.18], MX: [-99.13, 19.43], MY: [101.69, 3.14], NG: [3.38, 6.52], NL: [4.9, 52.37],
  NO: [10.75, 59.91], NP: [85.32, 27.72], NZ: [174.76, -36.85], OM: [58.41, 23.59], PA: [-79.52, 8.98],
  PE: [-77.04, -12.05], PH: [120.98, 14.6], PK: [73.05, 33.68], PL: [21.01, 52.23], PR: [-66.11, 18.47],
  PT: [-9.14, 38.72], PY: [-57.58, -25.26], QA: [51.53, 25.29], RO: [26.1, 44.43], RS: [20.46, 44.82],
  RU: [37.62, 55.76], SA: [46.68, 24.71], SE: [18.07, 59.33], SG: [103.82, 1.35], SI: [14.51, 46.06],
  SK: [17.11, 48.15], SM: [12.46, 43.94], TH: [100.5, 13.76], TR: [28.98, 41.01], TW: [121.57, 25.03],
  UA: [30.52, 50.45], UG: [32.58, 0.35], US: [-118.24, 34.05], UY: [-56.16, -34.9], UZ: [69.24, 41.3],
  VA: [12.45, 41.9], VE: [-66.9, 10.48], VG: [-64.62, 18.43], VI: [-64.9, 18.34], VN: [105.83, 21.03],
  ZA: [28.05, -26.2], ZM: [28.29, -15.42], ZW: [31.05, -17.83],
  UK: [-0.13, 51.51],
}

const COORD_RE = /^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*$/

export function parseLocation(raw?: string | null): GeoPoint | null {
  const value = String(raw || '').trim()
  if (!value) return null
  const coord = value.match(COORD_RE)
  if (coord) {
    const lat = Number(coord[1])
    const lon = Number(coord[2])
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null
    if (lat < -90 || lat > 90 || lon < -180 || lon > 180) return null
    return { lon, lat }
  }
  const code = value.toUpperCase()
  const seen = new Set<string>()
  let cursor = code
  while (LOCATION_ALIAS[cursor] && !seen.has(cursor)) {
    seen.add(cursor)
    cursor = LOCATION_ALIAS[cursor]!
  }
  const iata = IATA_COORDS[cursor] || IATA_COORDS[COUNTRY_IATA[code] || ''] || IATA_COORDS[COUNTRY_IATA[cursor] || '']
  if (iata) return { lon: iata[0], lat: iata[1] }
  const region = REGION_COORDS[code] || REGION_COORDS[cursor]
  if (region) return { lon: region[0], lat: region[1] }
  const pair = COORDS[code]
  if (!pair) return null
  return { lon: pair[0], lat: pair[1] }
}

export function sphericalMean(points: GeoPoint[]): GeoPoint | null {
  if (!points.length) return null
  let x = 0
  let y = 0
  let z = 0
  for (const point of points) {
    const lon = point.lon * Math.PI / 180
    const lat = point.lat * Math.PI / 180
    const cos = Math.cos(lat)
    x += cos * Math.cos(lon)
    y += cos * Math.sin(lon)
    z += Math.sin(lat)
  }
  const n = points.length
  x /= n
  y /= n
  z /= n
  return {
    lon: Math.atan2(y, x) * 180 / Math.PI,
    lat: Math.atan2(z, Math.sqrt(x * x + y * y)) * 180 / Math.PI,
  }
}

export function locationKey(point: GeoPoint): string {
  return `${point.lon.toFixed(1)},${point.lat.toFixed(1)}`
}

export function canonicalIata(raw?: string | null): string {
  const code = String(raw || '').trim().toUpperCase()
  if (!code) return ''
  const seen = new Set<string>()
  let cursor = code
  while (LOCATION_ALIAS[cursor] && !seen.has(cursor)) {
    seen.add(cursor)
    cursor = LOCATION_ALIAS[cursor]!
  }
  if (IATA_COORDS[cursor]) return cursor
  return COUNTRY_IATA[code] || COUNTRY_IATA[cursor] || ''
}

export function cityLabel(code: string, locale: string): string {
  const names = IATA_NAMES[code]
  if (!names) return code
  return locale.toLowerCase().startsWith('zh') ? names.zh : names.en
}

export function locationOptions(locale: string): Array<{ code: string; label: string }> {
  return Object.keys(IATA_COORDS).map(code => ({
    code,
    label: `${cityLabel(code, locale)} · ${code}`,
  })).sort((a, b) => a.label.localeCompare(b.label, locale))
}

export function formatLocation(raw?: string | null, locale = 'zh-CN'): string {
  const value = String(raw || '').trim()
  if (!value) return ''
  const coord = value.match(COORD_RE)
  if (coord) return `${Number(coord[1]).toFixed(2)}, ${Number(coord[2]).toFixed(2)}`
  const iata = canonicalIata(value)
  if (iata) {
    const name = cityLabel(iata, locale)
    return name === iata ? iata : `${name} · ${iata}`
  }
  const code = value.toUpperCase()
  if (CONTINENTS.has(code)) return code
  try {
    const name = new Intl.DisplayNames([locale], { type: 'region' }).of(code === 'UK' ? 'GB' : code)
    if (name && name !== code) return name
  } catch { /* ignore */ }
  return value
}

function asObject(value: unknown): Record<string, unknown> {
  if (typeof value === 'string') {
    try { return asObject(JSON.parse(value) as unknown) }
    catch { return {} }
  }
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function text(value: unknown) {
  return value === undefined || value === null ? '' : String(value).trim()
}

export function publicNoteCustom(note: unknown): Record<string, unknown> {
  return asObject(asObject(note).customData)
}

/** 位置码 / ISO2 / IATA → ISO2（`UK`→`GB`）。无法识别则空。 */
export function iso2FromLocation(raw?: string | null): string {
  const upper = text(raw).toUpperCase()
  if (!upper) return ''
  const seen = new Set<string>()
  let cursor = upper
  while (LOCATION_ALIAS[cursor] && !seen.has(cursor)) {
    seen.add(cursor)
    cursor = LOCATION_ALIAS[cursor]!
  }
  if (cursor.length === 2) return cursor === 'UK' ? 'GB' : cursor
  if (upper.length === 2) return upper === 'UK' ? 'GB' : upper
  for (const [iso, iata] of Object.entries(COUNTRY_IATA)) {
    if (iata === cursor) return iso === 'UK' ? 'GB' : iso
  }
  for (const [from, to] of Object.entries(LOCATION_ALIAS)) {
    if (to === cursor && from.length === 2) return from === 'UK' ? 'GB' : from
  }
  return ''
}

/** 世界面数据无 HK/MO/TW 独立板块，点亮并入 CN。 */
export function highlightCountry(iso: string): string {
  if (iso === 'HK' || iso === 'MO' || iso === 'TW') return 'CN'
  return iso
}

/** 公开备注 latlng / 地区码优先，其次 host.CountryCode。对齐 Nazhua / aobobo。 */
export function resolveServerGeo(server: {
  host?: { CountryCode?: string }
  public_note?: unknown
}): { point: GeoPoint; country: string } | null {
  const custom = publicNoteCustom(server.public_note)
  const lat = text(custom.lat)
  const lng = text(custom.lng)
  const point = parseLocation(text(custom.latlng))
    || (lat && lng ? parseLocation(`${lat},${lng}`) : null)
    || parseLocation(text(custom.location))
    || parseLocation(server.host?.CountryCode)
  if (!point) return null
  const iso = iso2FromLocation(text(custom.location)) || iso2FromLocation(server.host?.CountryCode)
  return { point, country: iso ? highlightCountry(iso) : '' }
}
