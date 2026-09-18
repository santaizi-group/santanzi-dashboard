import { getPublicNetwork, type MonitorHistory } from '@santaizi/api'

const cache = new Map<number, MonitorHistory[]>()
const inflight = new Map<number, Promise<MonitorHistory[]>>()
const waiting: Array<() => void> = []
const LIMIT = 4
let active = 0

function acquire() {
  if (active < LIMIT) {
    active += 1
    return Promise.resolve()
  }
  return new Promise<void>((resolve) => {
    waiting.push(() => {
      active += 1
      resolve()
    })
  })
}

function release() {
  active = Math.max(0, active - 1)
  waiting.shift()?.()
}

export function peekNetworkHistory(id: number) {
  return cache.get(id)
}

export function rememberNetworkHistory(id: number, rows: MonitorHistory[]) {
  cache.set(id, rows)
}

export async function loadNetworkHistory(id: number, options?: { force?: boolean }) {
  if (!options?.force) {
    const hit = cache.get(id)
    if (hit) return hit
    const pending = inflight.get(id)
    if (pending) return pending
  }
  const request = (async () => {
    await acquire()
    try {
      if (!options?.force) {
        const cached = cache.get(id)
        if (cached) return cached
      }
      const rows = (await getPublicNetwork(id)).data || []
      cache.set(id, rows)
      return rows
    } finally {
      release()
      inflight.delete(id)
    }
  })()
  inflight.set(id, request)
  return request
}
