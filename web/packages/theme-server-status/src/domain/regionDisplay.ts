/**
 * ServerStatus 国家/地区展示：位置码与 ISO2 归一成可读名和合法旗帜。
 * 别名表与 Nazhua 点阵地图同源，但不引入 theme-nazhua 包依赖。
 */

/** IATA / 俗称 → 规范位置码。与 Nazhua `aliasMapping` 保持一致。 */
const ALIAS: Record<string, string> = {
  SGP: 'SIN',
  ICN: 'SEL',
  NRT: 'TYO',
  HND: 'TYO',
  KIX: 'OSA',
  PAR: 'CDG',
  MOW: 'SVO',
  CHI: 'ORD',
  SHA: 'PVG',
  CAN: 'CKG',
  CTU: 'TFU',
  BJS: 'PEK',
  HK: 'HKG',
  MO: 'MFM',
  TW: 'TPE',
  ASH: 'IAD',
}

/** ISO2 → 代表城市位置码。与 Nazhua `countryCodeMapping` 保持一致。 */
const COUNTRY_IATA: Record<string, string> = {
  CN: 'PEK',
  JP: 'TYO',
  SG: 'SIN',
  KR: 'SEL',
  MY: 'KUL',
  VN: 'HAN',
  IN: 'DEL',
  TH: 'BKK',
  AE: 'DXB',
  TR: 'IST',
  RO: 'OTP',
  LU: 'LUX',
  FR: 'CDG',
  RU: 'SVO',
  DE: 'FRA',
  NL: 'AMS',
  UK: 'LON',
  GB: 'LON',
  AU: 'SYD',
  US: 'LAX',
  CA: 'YYZ',
  MX: 'MEX',
  CL: 'SCQ',
  BR: 'GRU',
  IT: 'MXP',
  ES: 'MAD',
  PL: 'WAW',
  BG: 'SOF',
  LT: 'VNO',
  NO: 'OSL',
  MA: 'RBA',
}

const IATA_TO_ISO2: Record<string, string> = {}
for (const [iso, iata] of Object.entries(COUNTRY_IATA)) {
  IATA_TO_ISO2[iata] = iso === 'UK' ? 'GB' : iso
}
for (const [from, to] of Object.entries(ALIAS)) {
  if (from.length === 2) IATA_TO_ISO2[to] = from === 'UK' ? 'GB' : from
}

const FLAG_SUBDIVISION = /^[a-z]{2}-[a-z]{2,8}$/
const LETTERS_2 = /^[A-Za-z]{2}$/
const LETTERS_2_3 = /^[A-Za-z]{2,3}$/

function text(value: unknown) {
  return value === undefined || value === null ? '' : String(value).trim()
}

function canonicalize(raw: string) {
  let code = raw.trim().toUpperCase()
  const seen = new Set<string>()
  while (code && !seen.has(code)) {
    seen.add(code)
    const next = ALIAS[code]
    if (!next || next === code) break
    code = next
  }
  return code
}

/** 2–3 字母或 `gb-eng` 这类旗帜细分码，不当作给人看的地名。 */
export function isRegionCode(value: unknown) {
  const raw = text(value)
  if (!raw) return false
  if (FLAG_SUBDIVISION.test(raw.toLowerCase())) return true
  return LETTERS_2_3.test(raw)
}

/** flag-icons 用的 ISO2 / 细分码；`uk` → `gb`。三字母位置码返回空。 */
export function normalizeFlagCode(value: unknown) {
  const raw = text(value).toLowerCase()
  if (!raw) return ''
  if (FLAG_SUBDIVISION.test(raw)) return raw
  if (LETTERS_2.test(raw)) return raw === 'uk' ? 'gb' : raw
  return ''
}

/** 位置码、ISO2、俗称 → 小写 ISO2（或 `gb-eng`）。无法识别则空。 */
export function resolveIso2(value: unknown) {
  const raw = text(value)
  if (!raw) return ''
  const asFlag = normalizeFlagCode(raw)
  if (asFlag) return asFlag
  const canon = canonicalize(raw)
  if (canon.length === 2) return normalizeFlagCode(canon)
  const iso = IATA_TO_ISO2[canon]
  return iso ? normalizeFlagCode(iso) : ''
}

export function regionDisplayName(iso2: unknown, locale = 'zh-CN') {
  const iso = text(iso2)
  if (!iso) return ''
  const region = iso.includes('-') ? iso.slice(0, 2).toUpperCase() : iso.toUpperCase()
  if (!LETTERS_2.test(region)) return ''
  try {
    const short = new Intl.DisplayNames([locale], { type: 'region', style: 'short' }).of(region) || ''
    if (short && !isRegionCode(short)) return short
    const long = new Intl.DisplayNames([locale], { type: 'region', style: 'long' }).of(region) || ''
    if (long && !isRegionCode(long)) return long
    return long || short
  } catch {
    return ''
  }
}

export function resolveRegionLabel(
  locationLabel: unknown,
  location: unknown,
  countryCode?: unknown,
  locale = 'zh-CN',
) {
  const label = text(locationLabel)
  if (label && !isRegionCode(label)) return label
  const loc = text(location)
  if (loc && !isRegionCode(loc)) return loc
  const iso = resolveIso2(label) || resolveIso2(loc) || resolveIso2(countryCode)
  return regionDisplayName(iso, locale)
}

export function resolveFlagCode(flag: unknown, location?: unknown, countryCode?: unknown) {
  return normalizeFlagCode(flag) || resolveIso2(location) || resolveIso2(countryCode)
}
