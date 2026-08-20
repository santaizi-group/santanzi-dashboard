import { expect, test, type Page, type Route } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const list = (data: unknown[] = []) => JSON.stringify({ data, meta: { total: data.length } })
const item = (data: unknown) => JSON.stringify({ data })
const worldGeoJSON = readFileSync(resolve(process.cwd(), 'resource/static/theme-nazhua/maps/world.geo.json'), 'utf8')

const servers = [
  {
    id: 1, name: 'HKG-EDGE', tag: 'HKG', display_index: 30, hide_for_guest: false, enable_ddns: false, online: true,
    host: { Platform: 'debian', CountryCode: 'HK', CPU: ['AMD EPYC 2 Physical Core'], Arch: 'amd64', Version: '6.8', MemTotal: 2_147_483_648, DiskTotal: 21_474_836_480 },
    state: { CPU: 8.2, MemUsed: 767_557_632, DiskUsed: 6_442_450_944, Uptime: 2_074_200, NetInSpeed: 5_800, NetOutSpeed: 5_100, NetInTransfer: 98_320_000_000, NetOutTransfer: 63_740_000_000, Load1: 0.21, Load5: 0.4, Load15: 0.5, ProcessCount: 92, TcpConnCount: 16, UdpConnCount: 5 },
    public_note: { customData: { location: 'HKG', slogan: 'Hong Kong Premium', flag: 'hk' }, billingDataMod: { amount: '109.00CNY', cycle: '月' }, planDataMod: { networkRoute: 'IEPL,电信专线', IPv4: '1', IPv6: '1', trafficType: '2' } },
    telemetry: { host: 'online', connectivity: 'healthy', available: true, coverage: '1/1' },
  },
  {
    id: 2, name: 'SGP-BAGE', tag: 'SGP', display_index: 20, hide_for_guest: false, enable_ddns: false, online: true,
    host: { Platform: 'freebsd', CountryCode: 'SG', CPU: ['Intel 1 Physical Core'], Arch: 'amd64', Version: '14', MemTotal: 1_073_741_824, DiskTotal: 10_737_418_240 },
    state: { CPU: 1, MemUsed: 549_453_824, DiskUsed: 5_368_709_120, Uptime: 11_491_200, NetInSpeed: 22_200, NetOutSpeed: 14_800, NetInTransfer: 512_000_000_000, NetOutTransfer: 251_180_000_000 },
    // 无 flag：靠位置别名链 SGP → SIN → SG 出旗
    public_note: { customData: { location: 'SGP', slogan: 'Singapore Edge' }, billingDataMod: { amount: '2.59USD', cycle: '月' }, planDataMod: { networkRoute: 'CTCSCI,原生IP', IPv4: '1', IPv6: '1', trafficType: '2' } },
  },
  {
    id: 3, name: 'TYO-OFFLINE', tag: 'JPN', display_index: 10, hide_for_guest: false, enable_ddns: false, online: false,
    host: { Platform: 'linux', CPU: ['4 Physical Core'], Arch: 'arm64', Version: '6.6', MemTotal: 4_294_967_296, DiskTotal: 42_949_672_960 },
    state: { CPU: 0, MemUsed: 0, DiskUsed: 0, Uptime: 0, NetInSpeed: 0, NetOutSpeed: 0, NetInTransfer: 0, NetOutTransfer: 0 },
    // 探针没上报国家码（GeoIP 缺库），只有 IATA 位置码：旗帜必须仍能解析
    public_note: { customData: { location: 'TYO', slogan: 'Maintenance' }, billingDataMod: { amount: '4.00USD', cycle: '月' }, planDataMod: { networkRoute: 'BGP', IPv4: '1' } },
  },
]

async function fulfillJSON(route: Route, body: string, status = 200) {
  await route.fulfill({ contentType: 'application/json', body, status })
}

async function useNazhua(page: Page, mode: 'dark' | 'light' = 'dark') {
  await page.addInitScript(({ color }) => {
    localStorage.setItem('santaizi-public-theme', 'nazhua')
    localStorage.setItem('santaizi-status-theme', color)
    localStorage.setItem('santaizi-locale', 'zh-CN')
  }, { color: mode })
}

async function useServerStatus(page: Page, mode: 'dark' | 'light' = 'light') {
  await page.addInitScript(({ color }) => {
    localStorage.setItem('santaizi-public-theme', 'server-status')
    localStorage.setItem('santaizi-status-theme', color)
    localStorage.setItem('santaizi-locale', 'zh-CN')
  }, { color: mode })
}

async function useNazhuaFromQuery(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('santaizi-public-theme', 'nazhua')
    localStorage.setItem('santaizi-locale', 'zh-CN')
    localStorage.setItem('santaizi-status-theme', new URL(window.location.href).searchParams.get('visual-mode') || 'dark')
  })
}

const nazhuaScreenshot = {
  animations: 'disabled' as const,
  fullPage: false,
  maxDiffPixelRatio: 0.01,
  timeout: 8_000,
}

async function waitNazhuaHomeReady(page: Page, mode: 'dark' | 'light') {
  await expect(page.locator('html')).toHaveAttribute('data-theme', mode)
  await expect(page.locator('.nazhua-card').first()).toBeVisible()
  await page.evaluate(async () => {
    await document.fonts.ready
    const urls = new Set<string>()
    const take = (value: string) => {
      for (const match of value.matchAll(/url\((['"]?)(.*?)\1\)/g)) {
        if (match[2] && match[2] !== 'none') urls.add(new URL(match[2], location.href).href)
      }
    }
    const shell = document.querySelector<HTMLElement>('.nazhua-shell')
    if (shell) {
      take(getComputedStyle(shell).getPropertyValue('--nazhua-bg-image'))
      take(getComputedStyle(shell, ':before').backgroundImage)
    }
    const map = document.querySelector<HTMLElement>('.nazhua-world-map__image')
    if (map) {
      const style = getComputedStyle(map)
      take(style.webkitMaskImage || style.maskImage)
    }
    await Promise.all([...urls].map(src => {
      const img = new Image()
      img.src = src
      return img.decode().catch(() => undefined)
    }))
  })
}

test.beforeEach(async ({ page }) => {
  await page.route('**/static/logo.svg', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><path fill="#2563eb" d="M4 4h24v24H4z"/></svg>' }))
  await page.route('**/static/theme-server-status/**', route => route.fulfill({ status: 204 }))
  await page.route('**/static/theme-nazhua/maps/world.geo.json', route => fulfillJSON(route, worldGeoJSON))
  await page.routeWebSocket('**/ws/v2/public/runtime', () => {})
  await page.route('**/api/v2/public/bootstrap', route => fulfillJSON(route, item({
    brand: '三太子监控', locale: 'zh-CN', version: 'test', logo_url: '/static/logo.svg', primary_color: '#f0cb23',
    requires_view_password: false, view_password_verified: true, show_availability: true, authenticated: true,
    theme: 'nazhua', allow_frontend_theme_switch: true,
  })))
  await page.route('**/api/v2/public/servers', route => fulfillJSON(route, list(servers)))
  await page.route('**/api/v2/public/cycle-transfer**', route => {
    const cycleRows = [
      { policy_id: 1, server_id: 1, name: 'Monthly', direction: 'both', used_bytes: 173_999_036_989, quota_bytes: 1_099_511_627_776, remaining_bytes: 925_512_590_787, usage_percent: 15.8, warning_percent: 80, status: 'normal', window_start: '2026-08-01T00:00:00Z', window_end: '2026-09-01T00:00:00Z', next_reset_at: '2026-09-01T00:00:00Z' },
      { policy_id: 2, server_id: 2, name: 'Monthly', direction: 'both', used_bytes: 92_997_746_115, quota_bytes: 549_755_813_888, remaining_bytes: 456_758_067_773, usage_percent: 16.9, warning_percent: 80, status: 'normal', window_start: '2026-08-01T00:00:00Z', window_end: '2026-09-01T00:00:00Z', next_reset_at: '2026-09-01T00:00:00Z' },
    ]
    const serverId = Number(new URL(route.request().url()).searchParams.get('server_id') || 0)
    return fulfillJSON(route, list(serverId ? cycleRows.filter(row => row.server_id === serverId) : cycleRows))
  })
  await page.route('**/api/v2/public/services', route => fulfillJSON(route, list([
    { id: 1, name: 'Public API', current_up: 99, current_down: 1, up: [99, 100, 98, 100], down: [1, 0, 2, 0], avg_delay: 42 },
    { id: 2, name: 'Cloudflare.V4', current_up: 100, current_down: 0, up: [100, 90], down: [0, 10], delay: [0, 0, 1.5860779] },
  ])))
  await page.route('**/api/v2/public/network/*', route => {
    const now = Date.now()
    return fulfillJSON(route, list([
      { monitor_name: 'ICMP', created_at: [now - 3_600_000, now - 1_800_000, now - 60_000], avg_delay: [42, 38, 40] },
    ]))
  })
  await page.route('**/api/v2/public/metrics/*', route => fulfillJSON(route, list([
    { window_start: '2026-08-13T08:00:00Z', cpu: 12, mem_used: 700_000_000, disk_used: 6_000_000_000, net_in_speed: 1000, net_out_speed: 800, process_count: 88, tcp_conn_count: 12, udp_conn_count: 4 },
    { window_start: '2026-08-13T08:01:00Z', cpu: 18, mem_used: 720_000_000, disk_used: 6_100_000_000, net_in_speed: 1200, net_out_speed: 900, process_count: 92, tcp_conn_count: 16, udp_conn_count: 5 },
  ])))
  await page.route('**/api/v2/public/servers/*/availability**', route => fulfillJSON(route, item({
    server_id: 1, days: 30, offline_count: 0, total_offline_seconds: 0, longest_offline_seconds: 0, availability_percent: 99.9,
  })))
})

test('public HTML declares the shared product favicon', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', '/static/logo.svg')
})

test('renders a complete Nazhua homepage with one shell, map points and cycle-aware cards', async ({ page }) => {
  await useNazhua(page)
  let cycleRequests = 0
  page.on('request', request => {
    if (request.url().includes('/api/v2/public/cycle-transfer')) cycleRequests += 1
  })
  await page.goto('/')

  await expect(page.locator('.nazhua-shell')).toBeVisible()
  await expect(page.locator('.nazhua-header')).toHaveCount(1)
  await expect(page.locator('.status-nav')).toHaveCount(0)
  await expect(page.locator('.nazhua-world-map')).toBeVisible()
  expect(await page.locator('.nazhua-world-map__point').count()).toBeGreaterThanOrEqual(2)
  await expect(page.locator('.nazhua-card')).toHaveCount(3)
  // 三台分别覆盖显式 flag、位置别名链、无国家码的 IATA 位置
  await expect(page.locator('.nazhua-card .nazhua-flag.fi-hk')).toHaveCount(1)
  await expect(page.locator('.nazhua-card .nazhua-flag.fi-sg')).toHaveCount(1)
  await expect(page.locator('.nazhua-card .nazhua-flag.fi-jp')).toHaveCount(1)
  await expect(page.locator('.nazhua-card .nazhua-flag-fallback')).toHaveCount(0)
  await expect(page.locator('.nazhua-card').first().locator('.traffic strong')).toContainText('861.95')
  await expect(page.locator('.nazhua-card').first().locator('.nazhua-card__spec')).toContainText('2C2G20G')
  await expect(page.locator('.nazhua-card').first().locator('.nazhua-donut__text strong').nth(1)).toHaveText('732M')
  await expect(page.locator('.nazhua-card').first().locator('.nazhua-donut__caption').nth(1)).toHaveText('2048M')
  await expect(page.locator('.nazhua-card').first().locator('.in strong')).toHaveText(/5\.7K/)
  await expect(page.locator('.nazhua-card').first().locator('.in strong')).not.toContainText('/s')
  await expect.poll(() => cycleRequests).toBe(1)

  const layout = await page.evaluate(() => {
    const map = document.querySelector<HTMLElement>('.nazhua-world-map')!
    const mapImage = map.querySelector<HTMLElement>('.nazhua-world-map__image')!
    const card = document.querySelector<HTMLElement>('.nazhua-card')!
    const fontSizes = [...document.querySelectorAll<HTMLElement>('.nazhua-shell *')]
      .filter(node => getComputedStyle(node).display !== 'none')
      .map(node => Number.parseFloat(getComputedStyle(node).fontSize))
      .filter(Number.isFinite)
    return {
      mapBackground: (() => {
        const style = getComputedStyle(mapImage)
        return style.webkitMaskImage || style.maskImage
      })(),
      mapWidth: map.getBoundingClientRect().width,
      viewportWidth: window.innerWidth,
      cardTop: card.getBoundingClientRect().top,
      minimumFont: Math.min(...fontSizes),
      bodyHeight: document.body.scrollHeight,
    }
  })
  expect(layout.mapBackground).toContain('world-map')
  expect(layout.mapWidth).toBeGreaterThanOrEqual(layout.viewportWidth > 720 ? 940 : 340)
  expect(layout.cardTop).toBeLessThan(760)
  expect(layout.minimumFont).toBeGreaterThanOrEqual(12)
  expect(layout.bodyHeight).toBeGreaterThan(700)
})

test('Nazhua detail opens from a scrolled homepage at the top', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useNazhua(page)
  await page.setViewportSize({ width: 1440, height: 500 })
  await page.goto('/')
  await expect(page.locator('.nazhua-card').last()).toBeVisible()
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0)
  await page.locator('.nazhua-card').last().locator('.nazhua-card__main').click()
  await expect(page).toHaveURL(/\/server\/\d+$/)
  await expect(page.locator('.nazhua-detail')).toBeVisible()
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0)
})

test('search opens an AppDialog and details retain the Nazhua shell', async ({ page }) => {
  await useNazhua(page)
  await page.goto('/')
  await page.getByRole('button', { name: '搜索' }).click()
  const dialog = page.getByRole('dialog', { name: '搜索' })
  await expect(dialog).toBeVisible()
  await dialog.getByPlaceholder('搜索服务器名称、分组、系统或国家').fill('SGP')
  await dialog.getByRole('button', { name: /SGP-BAGE/ }).click()
  await expect(page).toHaveURL(/\/server\/2$/)
  await expect(page.locator('.nazhua-header')).toHaveCount(1)
  await expect(page.locator('.nazhua-detail')).toBeVisible()
  await expect(page.locator('.nazhua-world-map')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'SGP-BAGE' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '周期流量' })).toBeVisible()
  await expect(page.locator('.nazhua-cycle-transfer__item')).toHaveCount(1)
  await expect(page.locator('.nazhua-cycle-transfer__item')).toContainText('剩余')
  await expect(page.getByRole('heading', { name: '资源历史' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '网络监控' })).toBeVisible()
  await expect(page.locator('.nazhua-monitor__toolbar')).toContainText('聚合')
  await expect(page.locator('.nazhua-monitor__toolbar')).toContainText('刷新')
  await expect(page.locator('.nazhua-monitor__toolbar')).toContainText('削峰')

  const ranges = page.locator('.nazhua-monitor .nazhua-monitor__ranges')
  await expect(ranges.getByRole('button', { name: '24小时' })).toHaveClass(/el-button--primary/)
  await ranges.getByRole('button', { name: '1小时' }).click()
  await expect(ranges.getByRole('button', { name: '1小时' })).toHaveClass(/el-button--primary/)
  await expect(ranges.getByRole('button', { name: '24小时' })).not.toHaveClass(/el-button--primary/)

  await expect(page.locator('.nazhua-monitor__grid')).toBeVisible()
  await page.locator('.nazhua-monitor__switch').filter({ hasText: '聚合' }).locator('.el-switch').click()
  await expect(page.locator('.nazhua-monitor__grid')).toHaveCount(0)
  await expect(page.locator('.nazhua-monitor__chart')).toHaveCount(1)
})

test('Nazhua details hide cycle transfer when the host has no policy', async ({ page }) => {
  await useNazhua(page)
  await page.goto('/server/3')
  await expect(page.locator('.nazhua-detail')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'TYO-OFFLINE' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '服务器信息' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '周期流量' })).toHaveCount(0)
})

test('Nazhua resource history range control reloads metrics for the selected window', async ({ page }) => {
  await useNazhua(page)
  const metricsUrls: string[] = []
  page.on('request', request => {
    if (request.url().includes('/api/v2/public/metrics/')) metricsUrls.push(request.url())
  })
  await page.goto('/server/1')
  await expect(page.locator('.nazhua-history__card')).toHaveCount(6)
  await expect(page.locator('.nazhua-history .el-date-editor')).toBeVisible()
  await expect(page.locator('.nazhua-history')).not.toContainText('最近')
  await expect(page.locator('.nazhua-history .el-button-group')).toHaveCount(0)
  await expect.poll(() => metricsUrls.some(url => {
    const parsed = new URL(url)
    return parsed.searchParams.has('start') && parsed.searchParams.has('end')
  })).toBe(true)
  await expect(page.locator('.nazhua-history__grid')).toBeVisible()
})

test('function menu keeps service and network pages inside Nazhua and switches shell cleanly', async ({ page }) => {
  await useNazhua(page)
  await page.goto('/')
  await page.getByRole('button', { name: '操作' }).click()
  await page.getByRole('menuitem', { name: /服务状态/ }).click()
  await expect(page).toHaveURL(/\/service$/)
  await expect(page.locator('.nazhua-header')).toHaveCount(1)
  await expect(page.getByText('Public API')).toBeVisible()
  await expect(page.getByText('42.00 ms')).toBeVisible()
  await expect(page.getByText('1.59 ms')).toBeVisible()
  await expect(page.locator('.service-panel')).not.toContainText('[')
  await expect(page.locator('.svc-spark')).toHaveCount(2)

  await page.getByRole('button', { name: '操作' }).click()
  await page.getByRole('menuitem', { name: /网络/ }).click()
  await expect(page).toHaveURL(/\/network$/)
  await expect(page.locator('.nazhua-shell .network-panel')).toBeVisible()
  await expect(page.locator('.network-server-select')).toHaveCount(0)
  await expect(page.locator('.network-tile')).toHaveCount(3)
  await expect(page.locator('.network-grid')).toHaveAttribute('data-density', 'few')
  await expect(page.getByRole('button', { name: 'HKG-EDGE' })).toBeVisible()

  await page.getByRole('button', { name: '操作' }).click()
  await page.getByRole('menuitem', { name: 'ServerStatus' }).click()
  await expect(page.locator('.server-status-shell')).toBeVisible()
  await expect(page.locator('.status-nav')).toHaveCount(1)
  await expect(page.locator('.nazhua-header')).toHaveCount(0)
})

test('ServerStatus service cards show a single latency value and sparkline', async ({ page }) => {
  await useServerStatus(page)
  await page.goto('/service')
  await expect(page.locator('.server-status-shell .service-panel')).toBeVisible()
  await expect(page.getByText('Public API')).toBeVisible()
  await expect(page.getByText('Cloudflare.V4')).toBeVisible()
  await expect(page.getByText('42.00 ms')).toBeVisible()
  await expect(page.getByText('1.59 ms')).toBeVisible()
  await expect(page.locator('.service-panel')).not.toContainText('[')
  await expect(page.locator('.svc-spark')).toHaveCount(2)
})

test('ServerStatus network page tiles hosts and opens a latency drawer', async ({ page }, testInfo) => {
  await useServerStatus(page)
  await page.goto('/network')
  await expect(page.locator('.network-server-select')).toHaveCount(0)
  await expect(page.locator('.network-tile')).toHaveCount(3)
  await expect(page.locator('.network-grid')).toHaveAttribute('data-density', 'few')
  await expect(page.getByRole('button', { name: 'HKG-EDGE' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'SGP-BAGE' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'TYO-OFFLINE' })).toBeVisible()
  const tileWidth = await page.locator('.network-tile').first().evaluate((node) => node.getBoundingClientRect().width)
  expect(tileWidth).toBeGreaterThan(280)
  if (testInfo.project.name === 'status-desktop') {
    const fill = await page.locator('.network-panel').evaluate((panel) => {
      const grid = panel.querySelector('.network-grid')
      if (!(grid instanceof HTMLElement)) return 0
      return grid.getBoundingClientRect().height / panel.getBoundingClientRect().height
    })
    expect(fill).toBeGreaterThan(0.7)
    const spark = page.locator('.network-tile .svc-spark').first()
    await expect(spark).toBeVisible()
    const sparkHeight = await spark.evaluate((node) => node.getBoundingClientRect().height)
    expect(sparkHeight).toBeGreaterThanOrEqual(36)
    expect(sparkHeight).toBeLessThanOrEqual(44)
  }
  await page.getByRole('button', { name: 'HKG-EDGE' }).click()
  await expect(page.locator('.el-drawer .ss-latency')).toBeVisible()
})

test('network tiles fetch history only for hosts near the viewport', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useServerStatus(page)
  const extra = Array.from({ length: 20 }, (_, index) => ({
    ...servers[0],
    id: 20 + index,
    name: `HOST-${String(index).padStart(2, '0')}`,
    display_index: index,
  }))
  await page.route('**/api/v2/public/servers', route => fulfillJSON(route, list([...servers, ...extra])))
  const requested = new Set<string>()
  page.on('request', (request) => {
    const match = request.url().match(/\/api\/v2\/public\/network\/(\d+)/)
    if (match) requested.add(match[1])
  })
  await page.setViewportSize({ width: 390, height: 640 })
  await page.goto('/network')
  await expect(page.locator('.network-tile').first()).toBeVisible()
  await expect.poll(() => requested.size).toBeGreaterThan(0)
  expect(requested.size).toBeLessThan(23)
})

test('hides the map without locations and keeps the first card near the header', async ({ page }) => {
  await useNazhua(page, 'light')
  await page.route('**/api/v2/public/servers', route => fulfillJSON(route, list([
    { ...servers[0], host: { Platform: 'linux', CountryCode: 'ZZZ' }, public_note: {}, name: 'NO-LOCATION' },
  ])))
  await page.goto('/')
  await expect(page.locator('.nazhua-world-map')).toHaveCount(0)
  await expect(page.getByText('NO-LOCATION')).toBeVisible()
  const top = await page.locator('.nazhua-card').evaluate(node => node.getBoundingClientRect().top)
  expect(top).toBeLessThan(230)
})

test('shows a retry action after a load failure and recovers', async ({ page }) => {
  await useNazhua(page)
  let attempts = 0
  await page.route('**/api/v2/public/servers', route => {
    attempts += 1
    return attempts === 1 ? fulfillJSON(route, item({}), 503) : fulfillJSON(route, list(servers))
  })
  await page.goto('/')
  await expect(page.getByText('请求失败，请稍后重试')).toBeVisible()
  await page.getByRole('button', { name: '刷新' }).click()
  await expect(page.locator('.nazhua-card')).toHaveCount(3)
  expect(attempts).toBe(2)
})

test('redirects protected sites to the themed password screen', async ({ page }) => {
  await useNazhua(page)
  let verified = false
  let unlocked = false
  await page.route('**/api/v2/public/bootstrap', route => fulfillJSON(route, item({
    brand: '三太子监控', locale: 'zh-CN', version: 'test', csrf_token: 'public-csrf', logo_url: '/static/logo.svg',
    requires_view_password: !unlocked, view_password_verified: unlocked, show_availability: true, authenticated: false,
    theme: 'nazhua', allow_frontend_theme_switch: true,
  })))
  await page.route('**/api/v2/public/view-password/session', route => {
    verified = route.request().headers()['x-csrf-token'] === 'public-csrf'
    unlocked = true
    return route.fulfill({ status: 204 })
  })
  await page.goto('/')
  await expect(page).toHaveURL(/\/view-password$/)
  await expect(page.locator('.nazhua-shell .password-card')).toBeVisible()
  await page.getByLabel('密码').fill('view-password')
  await page.getByRole('button', { name: '验证' }).click()
  await expect.poll(() => verified).toBe(true)
  await expect(page).toHaveURL(/\/$/)
})

test('mobile temporarily falls back to cards without discarding the saved desktop mode', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-mobile')
  await page.addInitScript(() => localStorage.setItem('santaizi-nazhua-list-mode', 'server-status'))
  await useNazhua(page)
  await page.goto('/')
  await expect(page.locator('.nazhua-home__list.mode-card')).toBeVisible()
  await expect(page.locator('.nazhua-card')).toHaveCount(3)
  expect(await page.evaluate(() => localStorage.getItem('santaizi-nazhua-list-mode'))).toBe('server-status')
  const header = await page.evaluate(() => {
    const inner = document.querySelector<HTMLElement>('.nazhua-header__inner')!
    const brand = document.querySelector<HTMLElement>('.nazhua-header__brand')!
    const stats = document.querySelector<HTMLElement>('.nazhua-header__stats')!
    return {
      viewport: window.innerWidth,
      flexWrap: getComputedStyle(inner).flexWrap,
      statsBasis: getComputedStyle(stats).flexBasis,
      statsOrder: getComputedStyle(stats).order,
      brandWidth: brand.getBoundingClientRect().width,
    }
  })
  expect(header).toMatchObject({ flexWrap: 'nowrap', statsBasis: 'auto', statsOrder: '0' })
  expect(header.brandWidth).toBeGreaterThan(160)
})

test('Nazhua color toggle sits in the header instead of the function menu', async ({ page }) => {
  await useNazhua(page)
  await page.goto('/')
  const toggle = page.locator('.nazhua-theme-button')
  await expect(toggle).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-label', '浅色')
  await page.locator('.nazhua-menu-button').click()
  await expect(page.getByRole('menuitem', { name: '浅色' })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: '深色' })).toHaveCount(0)
  await page.keyboard.press('Escape')
  await toggle.click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(toggle).toHaveAttribute('aria-label', '深色')
})

test('desktop exposes the card and ServerStatus list modes', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useNazhua(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')

  const modes = page.locator('.nazhua-filter__modes')
  await expect(page.locator('.nazhua-home__list.mode-card')).toBeVisible()
  await expect(modes.getByRole('button')).toHaveCount(2)
  await expect(modes.getByRole('button', { name: '列表' })).toHaveCount(0)
  const cardMark = await page.locator('.nazhua-card').first().evaluate(card => {
    const flag = card.querySelector('.nazhua-flag, .nazhua-flag-fallback')!.getBoundingClientRect()
    const os = card.querySelector('.nazhua-os-logo')!.getBoundingClientRect()
    return { flagWidth: flag.width, flagHeight: flag.height, osWidth: os.width, osHeight: os.height }
  })
  expect(cardMark.flagWidth).toBeGreaterThanOrEqual(30)
  expect(cardMark.flagWidth).toBeLessThanOrEqual(34)
  expect(cardMark.flagHeight).toBeGreaterThanOrEqual(19)
  expect(cardMark.flagHeight).toBeLessThanOrEqual(23)
  expect(cardMark.osWidth).toBeGreaterThanOrEqual(16)
  expect(cardMark.osWidth).toBeLessThanOrEqual(21)
  expect(cardMark.osHeight).toBeGreaterThanOrEqual(16)
  expect(cardMark.osHeight).toBeLessThan(cardMark.flagHeight)
  await modes.getByRole('button', { name: 'ServerStatus' }).click()
  await expect(page.locator('.nazhua-home__list.mode-server-status')).toBeVisible()
  const tableMark = await page.locator('.nazhua-status-table__row').first().evaluate(row => {
    const flag = row.querySelector('.nazhua-flag, .nazhua-flag-fallback')!.getBoundingClientRect()
    const os = row.querySelector('.nazhua-os-logo')!.getBoundingClientRect()
    return { flagWidth: flag.width, flagHeight: flag.height, osWidth: os.width, osHeight: os.height }
  })
  expect(tableMark.flagWidth).toBeGreaterThanOrEqual(30)
  expect(tableMark.flagWidth).toBeLessThanOrEqual(34)
  expect(tableMark.flagHeight).toBeGreaterThanOrEqual(19)
  expect(tableMark.flagHeight).toBeLessThanOrEqual(23)
  expect(tableMark.osWidth).toBeGreaterThanOrEqual(16)
  expect(tableMark.osWidth).toBeLessThanOrEqual(21)
  expect(tableMark.osHeight).toBeGreaterThanOrEqual(16)
  expect(tableMark.osHeight).toBeLessThan(tableMark.flagHeight)
  await expect(page.locator('.nazhua-status-table__head [role="columnheader"]')).toHaveCount(13)
  await expect(page.locator('.nazhua-status-table__row').first().getByRole('link')).toBeVisible()
  await expect(page.locator('.nazhua-status-table .ri-checkbox-circle-fill, .nazhua-status-table .ri-indeterminate-circle-fill')).toHaveCount(0)
  const tableGray = await page.locator('.nazhua-status-table__row').evaluateAll(rows => rows.map(row => {
    const style = getComputedStyle(row)
    return { offline: row.classList.contains('offline'), filter: style.filter, opacity: Number(style.opacity) }
  }))
  expect(tableGray.filter(row => row.offline)).toHaveLength(1)
  for (const row of tableGray) {
    if (row.offline) {
      expect(row.filter).toContain('grayscale')
      expect(row.opacity).toBeLessThan(1)
    } else {
      expect(row.filter === 'none' || !row.filter.includes('grayscale')).toBe(true)
      expect(row.opacity).toBe(1)
    }
  }
  const tableOverflow = await page.locator('.nazhua-status-table').evaluate(node => node.scrollWidth - node.clientWidth)
  expect(tableOverflow).toBeLessThanOrEqual(1)
  await modes.getByRole('button', { name: '卡片' }).click()
  await expect(page.locator('.nazhua-card')).toHaveCount(3)
})

test('Nazhua sort menu lists host network connection load and capacity keys', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useNazhua(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  await page.locator('.nazhua-filter__sort').click()
  await expect(page.locator('.nazhua-sort-menu__column')).toHaveCount(6)
  const sortMenu = await page.locator('.nazhua-sort-menu .el-dropdown-menu').boundingBox()
  expect(sortMenu!.height).toBeLessThan(sortMenu!.width)
  await expect(page.getByRole('menuitem', { name: '权重' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: '入网速度' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'TCP 连接' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: '内存总量' })).toBeVisible()
  await page.getByRole('menuitem', { name: '名称' }).click()
  expect(await page.evaluate(() => localStorage.getItem('santaizi-nazhua-sort-prop'))).toBe('name')
})

test('matches the upstream desktop track and map geometry at 1440px', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useNazhua(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')

  const geometry = await page.evaluate(() => {
    const map = document.querySelector<HTMLElement>('.nazhua-world-map')!.getBoundingClientRect()
    const filter = document.querySelector<HTMLElement>('.nazhua-filter')!.getBoundingClientRect()
    const card = document.querySelector<HTMLElement>('.nazhua-card')!.getBoundingClientRect()
    return {
      map: { x: map.x, y: map.y, width: map.width, height: map.height },
      filterY: filter.y,
      cardY: card.y,
      cardWidth: card.width,
    }
  })
  expect(geometry.map.x).toBeCloseTo(180, 0)
  expect(geometry.map.y).toBeCloseTo(80, 0)
  expect(geometry.map.width).toBeCloseTo(1080, 0)
  expect(geometry.map.height).toBeCloseTo(524, 0)
  expect(geometry.filterY).toBeCloseTo(614, 0)
  expect(geometry.cardY).toBeCloseTo(660, 0)
  expect(geometry.cardWidth).toBeGreaterThan(350)
  expect(geometry.cardWidth).toBeLessThan(355)
})

test('mobile controls keep 44px touch targets', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-mobile')
  await useNazhua(page)
  await page.goto('/')
  const chrome = page.locator('.nazhua-theme-button, .nazhua-menu-button, .nazhua-search-btn')
  expect(await chrome.count()).toBeGreaterThanOrEqual(3)
  for (let index = 0; index < await chrome.count(); index += 1) {
    const box = await chrome.nth(index).boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(44)
    expect(box?.height).toBeGreaterThanOrEqual(44)
  }
  const filterHeight = await page.locator('.nazhua-filter .el-button').first().evaluate(node => node.getBoundingClientRect().height)
  expect(filterHeight).toBeGreaterThanOrEqual(36)
  expect(filterHeight).toBeLessThanOrEqual(38)
  const filterRow = await page.evaluate(() => {
    const groups = document.querySelector<HTMLElement>('.nazhua-filter__groups')
    const sort = document.querySelector<HTMLElement>('.nazhua-filter__sort')
    if (!groups || !sort) return 99
    return Math.abs(groups.getBoundingClientRect().top - sort.getBoundingClientRect().top)
  })
  expect(filterRow).toBeLessThanOrEqual(2)
})

const nazhuaVisualCases = [
  { name: 'nazhua-dark-1920x947.png', width: 1920, height: 947, mode: 'dark' as const },
  { name: 'nazhua-dark-1440x900.png', width: 1440, height: 900, mode: 'dark' as const },
  { name: 'nazhua-dark-reference-1399x945.png', width: 1399, height: 945, mode: 'dark' as const },
  { name: 'nazhua-dark-mobile-390x844.png', width: 390, height: 844, mode: 'dark' as const },
  { name: 'nazhua-light-1920x947.png', width: 1920, height: 947, mode: 'light' as const },
  { name: 'nazhua-light-1440x900.png', width: 1440, height: 900, mode: 'light' as const },
  { name: 'nazhua-light-mobile-390x844.png', width: 390, height: 844, mode: 'light' as const },
]

test.describe('captures accepted Nazhua visual baselines', () => {
  // 8 张全页图不能共用默认 30s；串行避免 1920 截图互相抢 Vite。
  test.describe.configure({ mode: 'serial' })

  for (const visual of nazhuaVisualCases) {
    test(visual.name, async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== 'status-desktop')
      await useNazhuaFromQuery(page)
      await page.setViewportSize({ width: visual.width, height: visual.height })
      await page.goto(`/?visual-mode=${visual.mode}`)
      await page.evaluate(() => window.scrollTo(0, 0))
      await waitNazhuaHomeReady(page, visual.mode)
      await expect(page).toHaveScreenshot(visual.name, nazhuaScreenshot)
      if (visual.width === 390 && visual.mode === 'dark') {
        await page.goto('/server/1?visual-mode=dark')
        await expect(page.locator('.nazhua-detail')).toBeVisible()
        await expect(page).toHaveScreenshot('nazhua-detail-dark-mobile-390x844.png', nazhuaScreenshot)
      }
    })
  }
})

test('captures accepted Nazhua table and resource history baselines', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-desktop')
  await useNazhuaFromQuery(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  for (const mode of ['dark', 'light'] as const) {
    await page.goto(`/?visual-mode=${mode}`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', mode)
    await page.locator('.nazhua-filter__modes').getByRole('button', { name: 'ServerStatus' }).click()
    const table = page.locator('.nazhua-status-table')
    await expect(table).toBeVisible()
    expect(await table.evaluate(node => node.scrollWidth - node.clientWidth)).toBeLessThanOrEqual(1)
    await expect(table).toHaveScreenshot(`nazhua-table-${mode}-1440.png`, { animations: 'disabled', maxDiffPixelRatio: .01 })

    await page.goto(`/server/1?visual-mode=${mode}`)
    // 资源历史用固定时间戳，可作稳定基线；网络监控 mock 走当前时钟，不入基线。
    await expect(page.locator('.nazhua-history__card')).toHaveCount(6)
    await expect(page.locator('.nazhua-history .el-date-editor')).toBeVisible()
    await expect(page.locator('.nazhua-history .el-button-group')).toHaveCount(0)
    await expect(page.locator('.nazhua-history__chart canvas')).toHaveCount(6)
    // 工具栏改为起止范围；未授权 --update-snapshots，跳过 nazhua-history-*-1440.png
  }
})

test('ServerStatus shell uses a tokenized table without particle canvas', async ({ page }) => {
  await useServerStatus(page)
  await page.goto('/')
  await expect(page.locator('.server-status-shell')).toBeVisible()
  await expect(page.locator('.status-nav')).toHaveCount(1)
  await expect(page.locator('.status-particles, canvas.status-particles')).toHaveCount(0)
  await expect(page.locator('.ss-row')).toHaveCount(3)
  await expect(page.locator('.ss-card')).toHaveCount(0)
  await expect(page.getByText('主机管理')).toHaveCount(0)
  await expect(page.locator('.status-nav nav a.router-link-exact-active')).toHaveCount(1)
  const footer = page.locator('.server-status-shell > footer')
  await expect(footer).toContainText('三太子监控')
  await expect(footer).toContainText('test')
  await expect(footer.locator('a[href="https://github.com/santaizi-group/santanzi-dashboard"]')).toBeVisible()
  await expect(footer).not.toContainText('naiba')
  await expect(footer).not.toContainText('哪吒')
  await expect(page.locator('.ss-row').first().locator('.ss-chip--muted')).toHaveText('香港')
  await expect(page.locator('.ss-row').first().locator('.server-flag.fi-hk')).toBeVisible()
  await expect(page.locator('.ss-chip--muted').filter({ hasText: 'HKG' })).toHaveCount(0)
  await expect(page.locator('.ss-row').first()).toContainText('109.00CNY')
  await expect(page.locator('.ss-row').first()).toContainText('剩余 861.95G')
  await expect(page.locator('.ss-row').nth(2)).toContainText('不限制')
  await expect(page.locator('.ss-cell[data-label="位置"]')).toHaveCount(3)
  await expect(page.locator('.ss-cell[data-label="价格"]')).toHaveCount(3)
  await expect(page.locator('.ss-cell[data-label="剩余"]')).toHaveCount(0)
  await expect(page.locator('.ss-cell[data-label="连接数"]')).toHaveCount(3)
  await expect(page.locator('.ss-row').first().locator('.ss-cell[data-label="连接数"]')).toHaveText('16 / 5')
  await expect(page.locator('.ss-row').nth(1).locator('.ss-cell[data-label="连接数"]')).toHaveText('—')
  await expect(page.locator('.ss-row').nth(2).locator('.ss-cell[data-label="连接数"]')).toHaveText('—')
  await page.locator('.ss-row').first().locator('.ss-row__main').click()
  await expect(page.locator('.el-drawer .ss-detail')).toBeVisible()
  await expect(page.getByText('IEPL')).toBeVisible()
  await expect(page.locator('.meta-tag--billing').filter({ hasText: '109.00CNY' })).toBeVisible()
  await expect(page.locator('.ss-cycle').filter({ hasText: 'Monthly' })).toHaveCount(1)
  await expect(page.getByText('0.21 / 0.40 / 0.50')).toBeVisible()
  await expect(page.locator('.ss-detail__availability')).toBeVisible()
  await expect(page.getByText('30 天可用率')).toBeVisible()
  await expect(page.locator('.ss-latency')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('.el-drawer .ss-detail')).not.toBeVisible()
})

test('ServerStatus mobile table does not require horizontal page scroll', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-mobile')
  await useServerStatus(page)
  await page.goto('/')
  await expect(page.locator('.ss-row')).toHaveCount(3)
  const headsHidden = await page.locator('.ss-table__head').evaluateAll(nodes =>
    nodes.length > 0 && nodes.every(node => getComputedStyle(node).display === 'none'))
  expect(headsHidden).toBe(true)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 2)
  expect(overflow).toBe(true)
  const fonts = await page.evaluate(() => [...document.querySelectorAll<HTMLElement>('.server-status-shell *')]
    .filter(node => getComputedStyle(node).display !== 'none')
    .map(node => Number.parseFloat(getComputedStyle(node).fontSize))
    .filter(size => Number.isFinite(size) && size > 0))
  expect(Math.min(...fonts)).toBeGreaterThanOrEqual(12)
  await expect(page.locator('.status-brand span')).toBeVisible()
  const logo = await page.locator('.status-brand img').evaluate(node => {
    const box = node.getBoundingClientRect()
    return { width: box.width, height: box.height }
  })
  expect(logo.width).toBeGreaterThanOrEqual(33)
  expect(logo.height).toBeGreaterThanOrEqual(33)
  expect(await page.locator('.status-nav__links').evaluate(node => getComputedStyle(node).display === 'none')).toBe(true)
  const toolbarActionsHidden = await page.locator('.status-toolbar__page-action').evaluateAll(nodes =>
    nodes.length > 0 && nodes.every(node => getComputedStyle(node).display === 'none'))
  expect(toolbarActionsHidden).toBe(true)
  await expect(page.locator('.group-title')).toHaveCount(3)
  await page.getByRole('button', { name: '打开导航' }).click()
  const menu = page.locator('.status-mobile-nav')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('link', { name: '服务器状态' })).toBeVisible()
  await expect(menu.getByRole('link', { name: '服务状态' })).toBeVisible()
  await expect(menu.getByRole('link', { name: '网络' })).toBeVisible()
  const bodyShift = await page.evaluate(() => Math.abs(document.body.getBoundingClientRect().width - window.innerWidth))
  expect(bodyShift).toBeLessThanOrEqual(2)
  const iconAlign = await menu.locator('a, button').evaluateAll(nodes => nodes.map(node => {
    const icon = node.querySelector('i')
    const text = node.querySelector('span')
    if (!icon || !text) return 99
    const iconBox = icon.getBoundingClientRect()
    const textBox = text.getBoundingClientRect()
    return Math.abs((iconBox.top + iconBox.bottom) / 2 - (textBox.top + textBox.bottom) / 2)
  }))
  expect(Math.max(...iconAlign)).toBeLessThanOrEqual(2)
  await menu.getByRole('button', { name: '列表显示' }).click()
  await expect(page.locator('.status-mobile-nav')).toHaveCount(0)
  await expect(page.locator('.group-title')).toHaveCount(0)
  await page.getByRole('button', { name: '打开导航' }).click()
  await page.locator('.status-mobile-nav').getByRole('button', { name: '世界地图' }).click()
  await expect(page.locator('.map-dialog')).toBeVisible()
  await page.locator('.map-dialog button[aria-label="关闭"]').click()
  await expect(page.locator('.map-dialog')).toBeHidden()
  await page.locator('.ss-row').first().locator('.ss-row__main').click()
  const drawer = page.locator('.el-drawer .ss-detail')
  await expect(drawer).toBeVisible()
  const grid = await drawer.locator('.ss-detail__grid').evaluate(node => {
    const shorts = [...node.children].filter(cell => !cell.classList.contains('is-span')) as HTMLElement[]
    return {
      columns: getComputedStyle(node).gridTemplateColumns.split(' ').length,
      shortPair: shorts.some((cell, index) => {
        const next = shorts[index + 1]
        return Boolean(next && Math.abs(cell.getBoundingClientRect().top - next.getBoundingClientRect().top) < 4)
      }),
    }
  })
  expect(grid.columns).toBe(2)
  expect(grid.shortPair).toBe(true)
})

test('Nazhua mobile detail wraps gauges and info into two columns', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'status-mobile')
  await useNazhua(page)
  await page.goto('/server/1')
  await expect(page.locator('.nazhua-detail')).toBeVisible()
  const layout = await page.evaluate(() => {
    const metrics = [...document.querySelectorAll<HTMLElement>('.nazhua-detail-status__metric')]
    const info = [...document.querySelectorAll<HTMLElement>('.nazhua-info-box dl > div:not(.is-span)')]
    const fonts = [...document.querySelectorAll<HTMLElement>('.nazhua-detail *')]
      .filter(node => getComputedStyle(node).display !== 'none')
      .map(node => Number.parseFloat(getComputedStyle(node).fontSize))
      .filter(size => Number.isFinite(size) && size > 0)
    const tops = metrics.map(node => node.getBoundingClientRect().top)
    return {
      metricWrap: metrics.length <= 2
        || (metrics.length === 3 && Math.abs(tops[0] - tops[2]) < 8)
        || (metrics.length >= 4 && tops[2] - tops[0] > 40),
      infoColumns: info.length >= 2
        && Math.abs(info[0].getBoundingClientRect().top - info[1].getBoundingClientRect().top) < 4,
      overflow: document.documentElement.scrollWidth <= window.innerWidth + 2,
      minimumFont: Math.min(...fonts),
    }
  })
  expect(layout.metricWrap).toBe(true)
  expect(layout.infoColumns).toBe(true)
  expect(layout.overflow).toBe(true)
  expect(layout.minimumFont).toBeGreaterThanOrEqual(12)
})

test('ServerStatus hides availability when guests are not allowed to see it', async ({ page }) => {
  await useServerStatus(page)
  await page.route('**/api/v2/public/bootstrap', route => fulfillJSON(route, item({
    brand: '三太子监控', locale: 'zh-CN', version: 'test', logo_url: '/static/logo.svg', primary_color: '#2563eb',
    requires_view_password: false, view_password_verified: true, show_availability: false, authenticated: true,
    theme: 'server-status', allow_frontend_theme_switch: true,
  })))
  await page.goto('/')
  await expect(page.locator('.ss-row')).toHaveCount(3)
  await page.locator('.ss-row').first().locator('.ss-row__main').click()
  await expect(page.locator('.el-drawer .ss-detail')).toBeVisible()
  await expect(page.getByText('可用性')).toHaveCount(0)
  await expect(page.getByText('30 天可用率')).toHaveCount(0)
})
