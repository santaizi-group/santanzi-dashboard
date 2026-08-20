import { describe, expect, it } from 'vitest'
import { DEFAULT_FAVICON_HREF, resolveFaviconHref } from './favicon'

describe('public favicon', () => {
  it('defaults to the shared product logo', () => {
    expect(resolveFaviconHref()).toBe(DEFAULT_FAVICON_HREF)
    expect(resolveFaviconHref('')).toBe(DEFAULT_FAVICON_HREF)
    expect(resolveFaviconHref('https://example.com/logo.svg')).toBe(DEFAULT_FAVICON_HREF)
  })

  it('follows a sanitized site logo', () => {
    expect(resolveFaviconHref('/static/custom.svg')).toBe('/static/custom.svg')
    expect(resolveFaviconHref('data:image/svg+xml;base64,AAAA')).toBe('data:image/svg+xml;base64,AAAA')
  })
})
