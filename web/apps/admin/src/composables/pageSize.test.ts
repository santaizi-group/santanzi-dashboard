import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { pageSizeStorageKey, readStoredPageSize, writeStoredPageSize } from './pageSize'

const path = '/servers'
const memory = new Map<string, string>()

beforeEach(() => {
  memory.clear()
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key: string) => memory.get(key) ?? null,
      setItem: (key: string, value: string) => { memory.set(key, value) },
      removeItem: (key: string) => { memory.delete(key) },
    },
  })
})

afterEach(() => {
  memory.clear()
})

describe('pageSize', () => {
  it('restores a valid stored size and ignores junk', () => {
    expect(readStoredPageSize(path)).toBe(20)
    writeStoredPageSize(path, 50)
    expect(readStoredPageSize(path)).toBe(50)
    writeStoredPageSize(path, 40)
    expect(readStoredPageSize(path)).toBe(40)
    writeStoredPageSize(path, 30)
    expect(readStoredPageSize(path)).toBe(30)
    localStorage.setItem(pageSizeStorageKey(path), '7')
    expect(readStoredPageSize(path)).toBe(20)
  })
})
