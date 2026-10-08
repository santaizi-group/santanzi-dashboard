export const PAGE_SIZES = [10, 20, 30, 40, 50, 100] as const
export const DEFAULT_PAGE_SIZE = 20

export function pageSizeStorageKey(path: string) {
  return `santaizi-admin-page-size:${path}`
}

export function readStoredPageSize(path: string, fallback = DEFAULT_PAGE_SIZE) {
  try {
    const value = Number(localStorage.getItem(pageSizeStorageKey(path)))
    if ((PAGE_SIZES as readonly number[]).includes(value)) return value
  } catch {
    /* private mode */
  }
  return fallback
}

export function writeStoredPageSize(path: string, size: number) {
  if (!(PAGE_SIZES as readonly number[]).includes(size)) return
  try {
    localStorage.setItem(pageSizeStorageKey(path), String(size))
  } catch {
    /* ignore quota */
  }
}
