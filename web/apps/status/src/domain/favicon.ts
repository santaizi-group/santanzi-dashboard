export const DEFAULT_FAVICON_HREF = '/static/logo.svg'

export function resolveFaviconHref(logoURL?: string | null): string {
  const value = logoURL?.trim() ?? ''
  if (value.startsWith('/static/') || value.startsWith('data:image/')) return value
  return DEFAULT_FAVICON_HREF
}

export function applyFaviconHref(href: string, head: ParentNode = document.head): void {
  let link = head.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    const doc = head.ownerDocument ?? document
    link = doc.createElement('link')
    link.rel = 'icon'
    head.append(link)
  }
  if (link.getAttribute('href') !== href) link.setAttribute('href', href)
}
