import type { ServerRecord } from '@santaizi/api'
import type { ServerSortProp } from '@santaizi/status-core'
import { flagCode, getBillAndPlan, publicLocation } from './publicNoteView'

export interface StatusNoteColumns {
  location: boolean
  price: boolean
  remaining: boolean
}

export interface StatusTableColumns extends StatusNoteColumns {
  availability: boolean
}

export type ColumnId =
  | 'status'
  | 'name'
  | 'platform'
  | 'location'
  | 'price'
  | 'online'
  | 'availability'
  | 'load'
  | 'conn'
  | 'speed'
  | 'traffic'
  | 'cores'
  | 'memory'
  | 'disk'
  | 'remaining'

const COLUMN_SORT: Partial<Record<ColumnId, ServerSortProp>> = {
  status: 'online',
  name: 'name',
  platform: 'platform',
  location: 'country_code',
  online: 'boot_time',
  load: 'load1',
  conn: 'total_conn_count',
  speed: 'net_out_speed',
  traffic: 'total_transfer',
  cores: 'cpu',
  memory: 'mem_used',
  disk: 'disk_used',
}

export function columnSortProp(id: ColumnId): ServerSortProp | undefined {
  return COLUMN_SORT[id]
}

const COLUMN_ORDER: ColumnId[] = [
  'status', 'name', 'platform', 'location', 'price', 'online', 'availability',
  'load', 'conn', 'speed', 'traffic', 'cores', 'memory', 'disk', 'remaining',
]

const COLUMN_TRACKS: Record<ColumnId, { track: string; min: number }> = {
  status: { track: '28px', min: 28 },
  name: { track: 'minmax(120px, 1.5fr)', min: 120 },
  platform: { track: 'minmax(88px, 0.9fr)', min: 88 },
  location: { track: 'minmax(84px, 0.8fr)', min: 84 },
  price: { track: 'minmax(88px, 1fr)', min: 88 },
  online: { track: '64px', min: 64 },
  availability: { track: '56px', min: 56 },
  load: { track: '48px', min: 48 },
  conn: { track: '72px', min: 72 },
  speed: { track: '104px', min: 104 },
  traffic: { track: 'minmax(96px, 1fr)', min: 96 },
  cores: { track: '56px', min: 56 },
  memory: { track: '56px', min: 56 },
  disk: { track: '56px', min: 56 },
  remaining: { track: '80px', min: 80 },
}

const GAP = 8
const PAD = 32

function isNoteColumnVisible(id: ColumnId, columns: StatusTableColumns) {
  if (id === 'location') return columns.location
  if (id === 'price') return columns.price
  if (id === 'availability') return columns.availability
  if (id === 'remaining') return columns.remaining
  return true
}

export function statusTableLayout(columns: StatusTableColumns) {
  const tracks = COLUMN_ORDER
    .filter((id) => isNoteColumnVisible(id, columns))
    .map((id) => COLUMN_TRACKS[id])
  const minWidth = tracks.reduce((sum, col) => sum + col.min, 0)
    + GAP * Math.max(0, tracks.length - 1)
    + PAD
  return {
    columns: tracks.map((col) => col.track).join(' '),
    minWidth,
    count: tracks.length,
  }
}

export function resolveStatusNoteColumns(
  servers: readonly Pick<ServerRecord, 'public_note' | 'host'>[],
  nowMs = Date.now(),
  locale?: string,
): StatusNoteColumns {
  let location = false
  let price = false
  let remaining = false
  for (const row of servers) {
    const country = row.host?.CountryCode
    if (!location && (publicLocation(row.public_note, country, locale) || flagCode(row.public_note, country))) {
      location = true
    }
    const bill = getBillAndPlan(row.public_note, nowMs)
    if (!price && bill.amountKind) price = true
    if (!remaining && bill.remainingKind) remaining = true
    if (location && price && remaining) break
  }
  return { location, price, remaining }
}
