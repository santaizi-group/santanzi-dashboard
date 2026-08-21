import { expect, test, type Locator, type Page, type Route } from '@playwright/test'
import { readFile } from 'node:fs/promises'

const list = (data: unknown[] = []) => JSON.stringify({ data, meta: { page: 1, page_size: 20, total: data.length } })
const item = (data: unknown) => JSON.stringify({ data })
const probeMetadata = {
  required: ['heartbeat', 'identity'],
  optional: [{ id: 'cpu', disable_flag: '--disable-cpu' }, { id: 'memory', disable_flag: '--disable-memory' }, { id: 'nat', disable_flag: '--disable-nat' }],
  presets: {
    standard_cloud: { cpu: true, memory: true, disk: true, network: true, connections: true, processes: true, temperature: false, gpu: false, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false },
    standard_physical: { cpu: true, memory: true, disk: true, network: true, connections: true, processes: true, temperature: true, gpu: true, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false },
    light: { cpu: true, memory: true, disk: true, network: true, connections: false, processes: false, temperature: false, gpu: false, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false },
    alive: { cpu: false, memory: false, disk: false, network: false, connections: false, processes: false, temperature: false, gpu: false, host_info: false, ip_report: false, http_probe: false, icmp_probe: false, tcp_probe: false, nat: false },
  },
}

async function fulfillJSON(route: Route, body: string, status = 200) {
  await route.fulfill({ contentType: 'application/json', status, body })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => { localStorage.setItem('santaizi-locale', 'zh-CN') })
  await page.route('**/static/logo.svg', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><path fill="#2563eb" d="M4 4h24v24H4z"/></svg>' }))
  // `/api/v2/**` only: `/api/**` also matches Vite modules under `/admin/src/api/`.
  await page.route('**/api/v2/**', route => {
    const path = new URL(route.request().url()).pathname
    const method = route.request().method()
    if (path.endsWith('/auth/session')) {
      return fulfillJSON(route, item({
        authenticated: true, csrf_token: 'test-csrf', login_url: '/oauth2/login', version: '1.2.3',
        capabilities: ['*'], user: { id: 1, login: 'admin', name: 'Admin', super_admin: true },
      }))
    }
    if (path.endsWith('/probe-capabilities')) return fulfillJSON(route, item(probeMetadata))
    if (method === 'GET') return fulfillJSON(route, list())
    return fulfillJSON(route, item({}), method === 'POST' || method === 'PUT' ? 201 : 200)
  })
})

async function chooseSelectValue(page: Page, dialog: Locator, label: string, value: string) {
  const field = dialog.getByLabel(label)
  await field.click()
  await field.fill(value)
  await page.getByRole('option', { name: value, exact: true }).click()
}

async function clickVisibleRowAction(page: Page, name: string) {
  await page.locator('.actions-more').filter({ visible: true }).first().click()
  await page.getByRole('menuitem', { name: new RegExp(name) }).filter({ visible: true }).click()
}

async function mockEditorOptions(page: Page) {
  await page.route('**/api/v2/admin/servers?**', route => fulfillJSON(route, list([{ id: 7, name: 'edge-a', tag: 'edge', online: true, public_note: {}, monitoring_options: {} }])))
  await page.route('**/api/v2/admin/notifications?**', route => fulfillJSON(route, list([{ id: 3, name: 'Ops', tag: 'ops', url: 'https://example.test/hook', method: 'post', request_type: 'json', headers: [], body: '', verify_tls: true }])))
}

async function mockServerEditorLookups(page: Page) {
  await page.route('**/api/v2/admin/server-groups**', route => fulfillJSON(route, list([{ name: 'edge' }])))
  await page.route('**/api/v2/admin/ddns?**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/notifications?**', route => fulfillJSON(route, list()))
}

async function waitForEditorReady(dialog: Locator) {
  await expect(dialog.getByLabel('服务器名称')).toBeVisible()
  await expect(dialog.locator('.el-loading-mask')).toHaveCount(0)
}

async function assertAdminContentScrolls(page: Page, lastItem: Locator) {
  const content = page.locator('#main-content')
  await expect(lastItem).toBeVisible()
  const metrics = await content.evaluate((el: HTMLElement) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
    windowY: window.scrollY,
    topbarTop: document.querySelector<HTMLElement>('.admin-topbar')!.getBoundingClientRect().top,
  }))
  expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight)
  expect(metrics.windowY).toBe(0)
  expect(metrics.topbarTop).toBeGreaterThanOrEqual(0)
  expect(metrics.topbarTop).toBeLessThan(2)
  await expect(lastItem).not.toBeInViewport()
  await content.evaluate((el: HTMLElement) => { el.scrollTop = el.scrollHeight })
  await expect.poll(() => content.evaluate((el: HTMLElement) => el.scrollTop)).toBeGreaterThan(0)
  await expect(lastItem).toBeInViewport()
  expect(await page.evaluate(() => window.scrollY)).toBe(0)
  expect(await page.locator('.admin-topbar').evaluate((el: HTMLElement) => el.getBoundingClientRect().top)).toBeLessThan(2)
}

async function assertAdminContentDoesNotScroll(page: Page) {
  const metrics = await page.locator('#main-content').evaluate((el: HTMLElement) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }))
  expect(metrics.scrollHeight).toBeLessThanOrEqual(metrics.clientHeight + 1)
}

test('creates a server with structured public notes and reusable installation credentials', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  await mockServerEditorLookups(page)
  await page.route('**/api/v2/admin/servers**', route => {
    if (route.request().method() === 'POST') {
      submitted = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ id: 2, ...submitted, secret: 'reusable-secret', online: false }), 201)
    }
    return fulfillJSON(route, list())
  })
  await page.route('**/api/v2/admin/probe-capabilities', route => fulfillJSON(route, item(probeMetadata)))
  await page.route('**/api/v2/admin/servers/2/install-preview', route => fulfillJSON(route, item({ platform: 'linux', command: 'install santaizi-agent --clean-install', clean_install: true, options: route.request().postDataJSON()?.options || {} })))

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '添加服务器' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务器' })
  await waitForEditorReady(dialog)
  await dialog.getByLabel('服务器名称').fill('edge-b')
  await chooseSelectValue(page, dialog, '分组', 'edge')
  await dialog.getByRole('tab', { name: '公开备注' }).click()
  await dialog.getByLabel('金额').fill('12.00')
  await dialog.getByRole('tab', { name: '套餐' }).click()
  await dialog.getByLabel('带宽').fill('1 Gbps')
  await dialog.getByRole('button', { name: '保存' }).click()

  await expect.poll(() => submitted).toMatchObject({ name: 'edge-b', tag: 'edge', public_note: { billingDataMod: { amount: '12.00' }, planDataMod: { bandwidth: '1 Gbps' } } })
  const install = page.getByRole('dialog', { name: /安装探针/ })
  await expect(install.getByLabel('密钥')).toHaveValue('reusable-secret')
  await expect(install.getByText('标准·云', { exact: true })).toBeVisible()
  await expect(install.getByText('标准·物理', { exact: true })).toBeVisible()
  await expect(install.getByText('轻量', { exact: true })).toBeVisible()
  await expect(install.getByText('仅存活', { exact: true })).toBeVisible()
  await expect(install.getByText('CPU 与负载', { exact: true })).toBeVisible()
})

test('exports and imports host backups from host management', async ({ page }) => {
  let imported: Record<string, unknown> | undefined
  await page.route('**/api/v2/admin/servers/export', route => fulfillJSON(route, item({
    format: 'santaizi.servers.v1',
    exported_at: '2026-08-20T00:00:00Z',
    servers: [{ name: 'edge-a', tag: 'edge', traffic_policies: [], secret: 'reusable-secret' }],
  })))
  await page.route('**/api/v2/admin/servers/import/preview', route => fulfillJSON(route, item({
    items: [
      { index: 0, name: 'edge-a', match: 'update', current_id: 7, changes: ['tag'], warnings: [], suggested_action: 'overwrite', allowed_actions: ['overwrite', 'skip'] },
      { index: 1, name: 'edge-b', match: 'create', changes: [], warnings: [], suggested_action: 'create', allowed_actions: ['create', 'skip'] },
    ],
  })))
  await page.route('**/api/v2/admin/servers/import', route => {
    imported = route.request().postDataJSON() as Record<string, unknown>
    return fulfillJSON(route, item({ created: 1, overwritten: 1, skipped: 0 }))
  })

  await page.goto('/admin/servers')
  await expect(page.getByRole('button', { name: '导出' })).toBeVisible()
  await expect(page.getByRole('button', { name: '导入' })).toBeVisible()

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出' }).click()
  const exportBox = page.getByRole('dialog').filter({ hasText: '探针认证密钥' })
  await expect(exportBox).toBeVisible()
  await exportBox.getByRole('button', { name: '确认' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toMatch(/^santaizi-servers-\d{8}\.json$/)
  const downloadPath = await download.path()
  expect(downloadPath).toBeTruthy()
  expect(await readFile(downloadPath!, 'utf8')).toContain('reusable-secret')

  await page.getByRole('button', { name: '导入' }).click()
  await page.locator('input[type="file"]').setInputFiles({
    name: 'backup.json',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify({
      format: 'santaizi.servers.v1',
      servers: [{ name: 'edge-a', tag: 'core' }, { name: 'edge-b' }],
    })),
  })
  const dialog = page.getByRole('dialog', { name: '导入主机' })
  await expect(dialog.getByText('edge-a')).toBeVisible()
  await expect(dialog.getByText('将覆盖', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '导入' }).click()
  await page.getByRole('button', { name: '确认' }).click()
  await expect.poll(() => imported).toMatchObject({
    actions: [
      { index: 0, action: 'overwrite' },
      { index: 1, action: 'create' },
    ],
  })
})

test('copies default agent install command from host management', async ({ page }) => {
  let preview: Record<string, unknown> | undefined
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async () => {} },
    })
  })
  await page.route('**/api/v2/admin/servers**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/install-preview')) {
      preview = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ platform: 'linux', command: 'install santaizi-agent --clean-install', clean_install: true, options: preview?.options || {} }))
    }
    return fulfillJSON(route, list([{ id: 2, name: 'edge-b', tag: 'edge', online: true, public_note: {}, monitoring_options: {} }]))
  })

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '复制安装命令' }).filter({ visible: true }).first().click()
  await expect(page.getByRole('dialog', { name: /安装探针/ })).toHaveCount(0)
  await expect(page.getByText('已复制').filter({ visible: true })).toBeVisible()
  await expect.poll(() => preview).toMatchObject({
    platform: 'linux',
    clean_install: true,
    options: probeMetadata.presets.standard_cloud,
  })
})

test('shows a copyable agent upgrade command from host management', async ({ page }) => {
  await page.route('**/api/v2/admin/servers**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/upgrade-preview')) {
      return fulfillJSON(route, item({ platform: 'linux', command: "curl -fsSL 'https://example.invalid/upgrade_agent.sh' | bash" }))
    }
    return fulfillJSON(route, list([{ id: 2, name: 'edge-b', tag: 'edge', online: true, public_note: {}, monitoring_options: {} }]))
  })

  await page.goto('/admin/servers')
  await clickVisibleRowAction(page, '升级探针')
  const dialog = page.getByRole('dialog', { name: /升级探针/ })
  await expect(dialog.locator('textarea')).toHaveValue("curl -fsSL 'https://example.invalid/upgrade_agent.sh' | bash")
  await expect(dialog.getByRole('button', { name: '复制升级命令' })).toBeVisible()
})

test('opens script commands drawer from the top bar', async ({ page }) => {
  await page.route('**/api/v2/admin/script-commands', route => fulfillJSON(route, item({
    commands: [
      { id: 'dashboard_upgrade', group: 'dashboard', platform: 'linux', command: 'cd /opt/santaizi && docker compose pull && docker compose up -d', destructive: false },
      { id: 'collector_upgrade', group: 'collector', platform: 'linux', command: "curl -fsSL 'https://example.invalid/upgrade_collector.sh' | bash", destructive: false },
      { id: 'agent_upgrade_linux', group: 'agent', platform: 'linux', command: "curl -fsSL 'https://example.invalid/upgrade_agent.sh' | bash", destructive: false },
    ],
  })))

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '脚本命令' }).click()
  const drawer = page.getByRole('dialog', { name: '脚本命令' })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByText('升级主面板', { exact: true })).toBeVisible()
  await expect(drawer.locator('textarea').first()).toHaveValue('cd /opt/santaizi && docker compose pull && docker compose up -d')
  await expect(drawer.locator('textarea').nth(1)).toHaveValue("curl -fsSL 'https://example.invalid/upgrade_collector.sh' | bash")
})

test('manages multiple traffic policies inside the server editor', async ({ page }) => {
  let payload: Record<string, unknown> | undefined
  let policyWrites = 0
  await mockServerEditorLookups(page)
  await page.route('**/api/v2/admin/servers/**/traffic-policies**', async route => {
    policyWrites += 1
    await fulfillJSON(route, item({}), 201)
  })
  await page.route('**/api/v2/admin/servers**', route => {
    if (route.request().method() === 'POST') {
      payload = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ id: 4, ...payload, secret: 'server-secret' }), 201)
    }
    return fulfillJSON(route, list())
  })
  await page.route('**/api/v2/admin/probe-capabilities', route => fulfillJSON(route, item(probeMetadata)))
  await page.route('**/api/v2/admin/servers/4/install-preview', route => fulfillJSON(route, item({ platform: 'linux', command: 'install santaizi-agent', clean_install: true, options: route.request().postDataJSON()?.options || {} })))

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '添加服务器' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务器' })
  await waitForEditorReady(dialog)
  await dialog.getByLabel('服务器名称').fill('traffic-node')
  await dialog.getByRole('tab', { name: '流量策略' }).click()
  await dialog.getByRole('button', { name: '添加流量策略' }).click()
  await dialog.getByRole('button', { name: '添加流量策略' }).click()
  const cards = dialog.locator('.traffic-policy-card')
  await cards.nth(0).getByLabel('名称').fill('Monthly total')
  await cards.nth(1).getByLabel('名称').fill('Inbound cap')
  await dialog.getByRole('button', { name: '保存' }).click()

  await expect.poll(() => payload).toBeTruthy()
  const policies = payload?.traffic_policies as Record<string, unknown>[]
  expect(policies).toHaveLength(2)
  expect(policies.map(policy => policy.name)).toEqual(['Monthly total', 'Inbound cap'])
  expect(policies.every(policy => policy.mode === 'recurring' && Boolean(policy.cycle_start))).toBe(true)
  expect(policyWrites).toBe(0)
})

test('an unnamed traffic policy blocks the save before any server is created', async ({ page }) => {
  let creates = 0
  await mockServerEditorLookups(page)
  await page.route('**/api/v2/admin/servers**', route => {
    if (route.request().method() !== 'POST') return fulfillJSON(route, list())
    creates += 1
    return fulfillJSON(route, item({ id: 4, ...route.request().postDataJSON(), secret: 'server-secret' }), 201)
  })

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '添加服务器' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务器' })
  await waitForEditorReady(dialog)
  await dialog.getByLabel('服务器名称').fill('traffic-node')
  await dialog.getByRole('tab', { name: '流量策略' }).click()
  await dialog.getByRole('button', { name: '添加流量策略' }).click()
  await dialog.getByRole('button', { name: '保存' }).click()

  await expect(page.locator('.el-message').filter({ hasText: '流量策略缺少名称' })).toBeVisible()
  await expect(dialog).toBeVisible()
  expect(creates).toBe(0)
})

test('a rejected traffic policy does not create the server', async ({ page }) => {
  let policyWrites = 0
  await mockServerEditorLookups(page)
  await page.route('**/api/v2/admin/servers/**/traffic-policies**', async route => {
    policyWrites += 1
    await fulfillJSON(route, item({}), 201)
  })
  await page.route('**/api/v2/admin/servers**', route => {
    if (route.request().method() === 'POST') {
      return fulfillJSON(route, JSON.stringify({ status: 400, code: 'invalid_traffic_policy', detail: 'traffic quota must be greater than zero' }), 400)
    }
    return fulfillJSON(route, list())
  })

  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '添加服务器' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务器' })
  await waitForEditorReady(dialog)
  await dialog.getByLabel('服务器名称').fill('traffic-node')
  await dialog.getByRole('tab', { name: '流量策略' }).click()
  await dialog.getByRole('button', { name: '添加流量策略' }).click()
  await dialog.locator('.traffic-policy-card').getByLabel('名称').fill('Monthly total')
  await dialog.getByRole('button', { name: '保存' }).click()

  await expect(page.locator('.el-notification').filter({ hasText: '流量策略无效' })).toBeVisible()
  await expect(dialog).toBeVisible()
  expect(policyWrites).toBe(0)
  await expect(page.locator('.el-table').getByText('traffic-node')).toHaveCount(0)
})

test('service monitor uses typed target, notification group and searchable server transfer', async ({ page }) => {
  let payload: Record<string, unknown> | undefined
  await mockEditorOptions(page)
  await page.route('**/api/v2/admin/monitors**', route => {
    if (route.request().method() === 'POST') {
      payload = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ id: 1, ...payload }), 201)
    }
    return fulfillJSON(route, list())
  })

  await page.goto('/admin/services')
  await page.getByRole('button', { name: '添加服务监控' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务监控' })
  await dialog.getByLabel('名称').fill('Website health')
  await dialog.getByLabel('目标').fill('https://example.test/health')
  await dialog.getByText('仅所选服务器', { exact: true }).click()
  await expect(dialog.locator('.el-transfer')).toBeVisible()
  await dialog.locator('.el-transfer-panel').first().getByText('edge-a', { exact: true }).click()
  await dialog.locator('.el-transfer__buttons .el-button:not(.is-disabled)').click()
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect.poll(() => payload).toMatchObject({ type: 'http', target: 'https://example.test/health', scope: { mode: 'include', server_ids: [7] } })
})

test('notification channels and alert rules have separate typed editors', async ({ page }) => {
  let notification: Record<string, unknown> | undefined
  let alert: Record<string, unknown> | undefined
  await mockEditorOptions(page)
  await page.route('**/api/v2/admin/notifications**', route => {
    if (route.request().method() === 'POST') {
      notification = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ id: 8, ...notification }), 201)
    }
    return fulfillJSON(route, list())
  })
  await page.route('**/api/v2/admin/alert-rules**', route => {
    if (route.request().method() === 'POST') {
      alert = route.request().postDataJSON() as Record<string, unknown>
      return fulfillJSON(route, item({ id: 9, ...alert }), 201)
    }
    return fulfillJSON(route, list())
  })
  await page.route('**/api/v2/admin/settings', route => fulfillJSON(route, item({})))

  await page.goto('/admin/notifications')
  await page.getByRole('button', { name: '添加通知渠道' }).click()
  let dialog = page.getByRole('dialog', { name: '添加通知渠道' })
  await dialog.getByLabel('名称').fill('Ops webhook')
  await dialog.getByLabel('通知组').fill('ops')
  await dialog.getByLabel('请求地址').fill('https://example.test/hook')
  await expect(dialog.getByLabel('通知组')).toHaveAttribute('type', 'text')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect.poll(() => notification).toMatchObject({ tag: 'ops', method: 'post', request_type: 'json' })

  await page.goto('/admin/alert-rules')
  await page.getByRole('button', { name: '添加告警规则' }).click()
  dialog = page.getByRole('dialog', { name: '添加告警规则' })
  await dialog.getByLabel('名称').fill('High CPU')
  await expect(dialog.getByText('CPU', { exact: true })).toBeVisible()
  await expect(dialog.getByLabel('指标')).toBeVisible()
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect.poll(() => alert).toMatchObject({ name: 'High CPU', trigger_mode: 'always', conditions: [{ type: 'cpu', duration_seconds: 30 }] })
})

test('additional features use provider metadata and server selectors', async ({ page }) => {
  await page.route('**/api/v2/admin/ddns**', route => fulfillJSON(route, list()))
  // Playwright evaluates matching routes in reverse registration order, so the
  // specific metadata route must be registered after the DDNS collection route.
  await page.route('**/api/v2/admin/ddns/providers', route => fulfillJSON(route, list([{ id: 1, name: 'Webhook', access_id: true, access_secret: true, webhook_url: true, webhook_method: true, webhook_request_type: true, webhook_headers: true, webhook_request_body: true }])))
  await page.route('**/api/v2/admin/nat**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/servers?**', route => fulfillJSON(route, list([{ id: 7, name: 'edge-a', tag: 'edge' }])))

  await page.goto('/admin/ddns')
  await page.getByRole('button', { name: '添加 DDNS 配置' }).click()
  let dialog = page.getByRole('dialog', { name: '添加 DDNS 配置' })
  await expect(dialog.getByLabel('服务商')).toBeVisible()
  await dialog.locator('.el-form-item').filter({ hasText: '服务商' }).locator('.el-select__wrapper').click()
  await page.getByRole('option', { name: 'Webhook' }).click()
  await expect(dialog.getByLabel('Access ID')).toHaveAttribute('type', 'text')
  await expect(dialog.getByLabel('Access Secret')).toHaveAttribute('type', 'password')
  await dialog.getByRole('button', { name: '取消' }).click()

  await page.goto('/admin/nat')
  await page.getByRole('button', { name: '添加内网穿透' }).click()
  dialog = page.getByRole('dialog', { name: '添加内网穿透' })
  await expect(dialog.getByLabel('服务器')).toHaveAttribute('role', 'combobox')
  await expect(dialog.getByLabel('本地服务')).toBeVisible()
  await expect(dialog.getByLabel('绑定域名')).toBeVisible()
})

test('dirty editor blocks escape and confirms cancellation', async ({ page }) => {
  await page.route('**/api/v2/admin/ddns?**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/notifications?**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/monitors**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list()))
  await page.goto('/admin/servers')
  await page.getByRole('button', { name: '添加服务器' }).click()
  const dialog = page.getByRole('dialog', { name: '添加服务器' })
  await waitForEditorReady(dialog)
  await dialog.getByLabel('服务器名称').fill('unsaved')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '取消' }).click()
  await expect(page.getByRole('dialog', { name: '放弃修改' })).toBeVisible()
  await page.getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '关闭此对话框' }).click()
  await expect(page.getByRole('dialog', { name: '放弃修改' }).last()).toBeVisible()
  await page.getByRole('dialog', { name: '放弃修改' }).last().getByRole('button', { name: '继续编辑' }).click()
  await page.locator('a[href="/admin/services"]').evaluate((element: HTMLElement) => element.click())
  const routeConfirm = page.getByRole('dialog', { name: '放弃修改' }).last()
  await expect(routeConfirm).toBeVisible()
  await routeConfirm.getByRole('button', { name: '放弃' }).click()
  await expect(page).toHaveURL(/\/admin\/services$/)
})

test('collector and API token credentials can be viewed again by stable identifier', async ({ page }) => {
  const collector = { id: 'collector-1', name: 'Shanghai edge', address: 'collector.example.com:5555', listen_port: 5556, tls: true, insecure_tls: false, generation: 1, config_version: 1, software_version: '1.4.0', status: 'online', revoked: false, connected_agents: 3, pending_records: 1, scopes: [{ type: 'all', value: '' }] }
  await page.route('**/api/v2/admin/telemetry/collectors**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/token')) return fulfillJSON(route, item({ collector_id: 'collector-1', registration_token: 'collector-token', revoked: false }))
    if (path.endsWith('/install-preview')) return fulfillJSON(route, item({ command: "curl -fsSL 'https://example.invalid/install_collector.sh' | bash -s -- --primary-endpoint '127.0.0.1:5555' --token 'collector-token' --grpc-port 5556 --primary-tls true --primary-insecure-tls false", primary_endpoint: '127.0.0.1:5555', grpc_port: 5556, primary_tls: true, primary_insecure_tls: false }))
    return fulfillJSON(route, list([collector]))
  })
  await page.goto('/admin/telemetry')
  await expect(page.getByText('连接探针').filter({ visible: true })).toBeVisible()
  await expect(page.getByText('collector.example.com').filter({ visible: true })).toBeVisible()
  await expect(page.getByText('访问端口').filter({ visible: true })).toBeVisible()
  await expect(page.getByText('监听端口').filter({ visible: true })).toBeVisible()
  await expect(page.getByText('5555', { exact: true }).filter({ visible: true })).toBeVisible()
  await expect(page.getByText('5556', { exact: true }).filter({ visible: true })).toBeVisible()
  await expect(page.getByText('从端版本').filter({ visible: true }).first()).toBeVisible()
  await expect(page.getByText('v1.4.0').filter({ visible: true }).first()).toBeVisible()
  await clickVisibleRowAction(page, '安装从端')
  const install = page.getByRole('dialog', { name: /安装从端/ })
  await expect(install.locator('textarea')).toHaveValue(/install_collector\.sh/)
  await expect(install.locator('textarea')).toHaveValue(/--primary-tls true/)
  await expect(install.locator('textarea')).toHaveValue(/--primary-insecure-tls false/)
  await expect(install.getByRole('button', { name: '复制安装命令' })).toBeEnabled()
  await install.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(install).toBeHidden()
  await clickVisibleRowAction(page, '查看 Token')
  await expect(page.getByRole('dialog', { name: '注册 Token' }).locator('input')).toHaveValue('collector-token')

  await page.route('**/api/v2/admin/api-tokens**', route => {
    const path = new URL(route.request().url()).pathname
    const base = { id: 17, note: 'automation', permission: 'write', enabled: true, expired: false, expires_at: null, created_at: '2026-08-11T12:00:00Z' }
    if (path.endsWith('/17')) return fulfillJSON(route, item({ ...base, token: 'reusable-api-token', token_prefix: 'reus…' }))
    return fulfillJSON(route, list([{ ...base, token_prefix: 'reus…' }]))
  })
  await page.goto('/admin/api-tokens')
  await clickVisibleRowAction(page, '查看 Token')
  await expect(page.getByRole('dialog', { name: 'Token' }).locator('input')).toHaveValue('reusable-api-token')
})

test('probe collector hides observer replication stats', async ({ page }) => {
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([
    {
      id: 'collector-1', name: 'Tokyo edge', address: 'observer.example.com:5555', listen_port: 5556, tls: true,
      generation: 1, config_version: 1, status: 'online', revoked: false, kind: 'observer',
      connected_agents: 31, pending_records: 6, spool_size: 646, last_seen: '2026-08-21T13:05:07Z',
    },
    {
      id: 'collector-2', name: 'LAX probe', address: '', tls: false, generation: 1, config_version: 1,
      status: 'online', revoked: false, kind: 'probe', connected_agents: 0, pending_records: 8072,
      spool_size: 726528, last_seen: '2026-08-21T13:05:07Z',
    },
  ])))
  await page.goto('/admin/telemetry')
  const table = page.locator('.el-table').filter({ visible: true })
  await expect(table.getByText('观测型')).toBeVisible()
  await expect(table.getByText('探测型')).toBeVisible()
  await expect(table.getByText('31', { exact: true })).toBeVisible()
  await expect(table.getByText('6', { exact: true })).toBeVisible()
  await expect(table).not.toContainText('8072')
  await expect(table).not.toContainText('709.5')
})

test('admin sidebar shows panel version', async ({ page }) => {
  await page.goto('/admin/')
  await expect(page.locator('.sidebar-version')).toHaveText('v1.2.3')
})

test('admin topbar collapses the sidebar', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/admin/')
  const toggle = page.locator('.admin-topbar .collapse-button')
  await expect(toggle).toBeVisible()
  await expect(page.locator('.admin-sidebar .collapse-button')).toHaveCount(0)
  await expect(page.locator('.admin-layout')).not.toHaveClass(/collapsed/)
  await toggle.click()
  await expect(page.locator('.admin-layout')).toHaveClass(/collapsed/)
  await expect(toggle).toHaveAttribute('aria-label', '展开导航')
  await toggle.click()
  await expect(page.locator('.admin-layout')).not.toHaveClass(/collapsed/)

  await page.setViewportSize({ width: 390, height: 667 })
  await expect(toggle).toBeHidden()
  await expect(page.locator('.mobile-menu')).toBeVisible()
})

test('switches locale without a page navigation', async ({ page }) => {
  await page.route('**/api/v2/admin/summary', route => fulfillJSON(route, item({})))
  await page.goto('/admin/')
  const before = page.url()
  await page.getByRole('button', { name: '语言' }).click()
  await page.getByText('English', { exact: true }).last().click()
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  expect(page.url()).toBe(before)
})

test('keeps light and dark surfaces coherent at all responsive baselines', async ({ page }) => {
  await page.route('**/api/v2/admin/summary', route => fulfillJSON(route, item({})))
  await page.goto('/admin/')

  const widths = [375, 768, 1024, 1440]
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(value => localStorage.setItem('santaizi-admin-theme', value), theme)
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 })
      await page.reload()
      await expect(page.locator('html')).toHaveClass(theme === 'dark' ? /dark/ : /^(?!.*dark)/)
      await expect(page.locator('.admin-sidebar')).toBeAttached()
      await expect(page.locator('.surface').first()).toBeVisible()
      await expect(page.locator('.page-head .el-button').first()).toBeVisible()
      await expect(page.locator('.mobile-menu')).toBeAttached()

      const measurements = await page.evaluate(() => {
        const rgb = (value: string) => value.match(/\d+(?:\.\d+)?/g)?.slice(0, 3).map(Number) ?? []
        const body = getComputedStyle(document.body)
        const sidebar = getComputedStyle(document.querySelector<HTMLElement>('.admin-sidebar')!)
        const surface = getComputedStyle(document.querySelector<HTMLElement>('.surface')!)
        const refresh = document.querySelector<HTMLElement>('.page-head .el-button')!
        const menu = getComputedStyle(document.querySelector<HTMLElement>('.mobile-menu')!)
        return {
          backgrounds: [rgb(body.backgroundColor), rgb(sidebar.backgroundColor), rgb(surface.backgroundColor)],
          refreshHeight: refresh.getBoundingClientRect().height,
          refreshFontSize: Number.parseFloat(getComputedStyle(refresh).fontSize),
          mobileMenuVisible: menu.display !== 'none',
        }
      })

      for (const color of measurements.backgrounds) {
        expect(color).toHaveLength(3)
        if (theme === 'light') expect(Math.min(...color)).toBeGreaterThanOrEqual(230)
        else expect(Math.max(...color)).toBeLessThanOrEqual(48)
      }
      expect(measurements.refreshHeight).toBeGreaterThanOrEqual(width <= 720 ? 44 : 36)
      expect(measurements.refreshFontSize).toBeGreaterThanOrEqual(12)
      expect(measurements.mobileMenuVisible).toBe(width <= 860)
    }
  }
})

test('overview counts live collectors and links to connection observation', async ({ page }) => {
  await page.route('**/api/v2/admin/summary', route => fulfillJSON(route, item({
    total_servers: 4, online_servers: 3, active_collectors: 2, collectors_offline: 1,
    paths_assigned: 4, paths_connected: 3, active_incidents: 0, data_loss: 0, telemetry_alerts: 1, telemetry_pending: 0,
  })))
  await page.route('**/api/v2/admin/settings', route => fulfillJSON(route, item({ site_title: '三太子监控', primary_location: 'CN' })))
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list([
    { id: 1, name: 'tokyo', tag: 'edge', display_index: 1, hide_for_guest: false, enable_ddns: false, online: true, host: { CountryCode: 'JP' }, public_note: { customData: { location: 'TYO' } } },
    { id: 2, name: 'ghost', tag: 'edge', display_index: 2, hide_for_guest: false, enable_ddns: false, online: false },
  ])))
  await page.route('**/api/v2/admin/telemetry/collectors', route => fulfillJSON(route, list([{
    id: 'collector-1', name: 'Shanghai edge', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    location: 'CN', generation: 1, config_version: 1, status: 'online', revoked: false, heartbeat_rtt_ms: 18,
    scopes: [{ type: 'all', value: '' }],
  }])))
  await page.route('**/api/v2/admin/connections/paths', route => fulfillJSON(route, list([
    {
      server_id: 1, server_name: 'tokyo', node_uuid: 'aa', observer_id: 'primary', observer_kind: 'primary', observer_name: '',
      assigned: true, sink: { connected: true, last_rtt_ms: 12 },
    },
    {
      server_id: 1, server_name: 'tokyo', node_uuid: 'aa', observer_id: 'collector-1', observer_kind: 'collector', observer_name: 'Shanghai edge',
      assigned: true, sink: { connected: true, last_rtt_ms: 20 },
    },
  ])))
  await page.goto('/admin/')
  await expect(page.locator('.metric-card').filter({ hasText: '在线从端' }).locator('strong')).toHaveText('2')
  await expect(page.getByRole('heading', { name: '全球链路' })).toBeVisible()
  // 陆地来自随包资产：只画出海洋与经纬网说明地图没加载成功。
  await page.waitForFunction(() => {
    const canvas = document.querySelector<HTMLCanvasElement>('.topology-globe__canvas')
    const ctx = canvas?.getContext('2d')
    if (!canvas || !ctx) return false
    const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height)
    const near = (index: number, rgb: number[], slack = 40) =>
      Math.abs(data[index]! - rgb[0]!) + Math.abs(data[index + 1]! - rgb[1]!) + Math.abs(data[index + 2]! - rgb[2]!) < slack
    let land = 0
    for (let index = 0; index < data.length; index += 4 * 13) {
      if (data[index + 3] && near(index, [0xff, 0xff, 0xff]) && !near(index, [0xd0, 0xe6, 0xfa])) land += 1
    }
    return land > 20
  })
  await expect(page.locator('.topology-legend')).toContainText('主面板')
  await expect(page.locator('.topology-globe__legend')).toContainText('射线')
  await expect(page.locator('.topology-globe__legend')).toContainText('自转')
  await expect(page.locator('.topology-legend-rays')).toContainText('从端节点')
  await expect(page.locator('.topology-globe__legend')).toBeVisible()
  await expect(page.locator('.topology-unlocated')).toContainText('1 个节点未定位')
  await expect(page.locator('.topology-panel > .section-title')).not.toContainText('详情')
  await expect(page.locator('.latency-panel')).toContainText('tokyo')
  await expect(page.locator('.latency-panel')).toContainText('12 ms')
  await expect(page.locator('.latency-panel')).toContainText('ghost')
  await expect(page.locator('.latency-panel')).toContainText('离线')
  await page.locator('.latency-row').filter({ hasText: 'tokyo' }).click()
  await expect(page.locator('.latency-row').filter({ hasText: 'tokyo' })).toHaveClass(/is-active/)
  await expect(page.locator('.latency-reset')).toBeVisible()
  await page.locator('.latency-reset').click()
  await expect(page.locator('.latency-row').filter({ hasText: 'tokyo' })).not.toHaveClass(/is-active/)
  await expect(page.locator('.overview-top .quick-grid')).toContainText('添加服务器')
  await expect(page.locator('.topology-panel')).not.toContainText('Shanghai edge')
  await page.locator('.latency-panel > .section-title').getByRole('link', { name: /详情/ }).click()
  await expect(page).toHaveURL(/\/admin\/connections$/)
})

test('overview latency list scrolls inside the panel on desktop', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'admin-mobile', 'viewport lock is desktop-only')
  const servers = Array.from({ length: 40 }, (_, index) => ({
    id: index + 1,
    name: `node-${String(index + 1).padStart(2, '0')}`,
    tag: 'edge',
    display_index: index + 1,
    hide_for_guest: false,
    enable_ddns: false,
    online: true,
    host: { CountryCode: 'JP' },
    public_note: { customData: { location: 'TYO' } },
  }))
  const paths = servers.map((server) => ({
    server_id: server.id,
    server_name: server.name,
    node_uuid: `node-${server.id}`,
    observer_id: 'primary',
    observer_kind: 'primary',
    observer_name: '',
    assigned: true,
    sink: { connected: true, last_rtt_ms: 12, rtt_sampled_at: '2026-08-17T06:00:00Z' },
  }))
  await page.route('**/api/v2/admin/summary', route => fulfillJSON(route, item({
    total_servers: 40, online_servers: 40, active_collectors: 0, collectors_offline: 0,
    paths_assigned: 40, paths_connected: 40, active_incidents: 0, data_loss: 0, telemetry_alerts: 0, telemetry_pending: 0,
  })))
  await page.route('**/api/v2/admin/settings', route => fulfillJSON(route, item({ site_title: '三太子监控', primary_location: 'CN' })))
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list(servers)))
  await page.route('**/api/v2/admin/telemetry/collectors', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/paths', route => fulfillJSON(route, list(paths)))
  await page.goto('/admin/')
  const listEl = page.locator('.latency-list')
  await expect(listEl.locator('.latency-row')).toHaveCount(40)
  await expect(listEl.getByText('node-40')).toBeAttached()
  await assertAdminContentDoesNotScroll(page)
  const listMetrics = await listEl.evaluate((el: HTMLElement) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }))
  expect(listMetrics.scrollHeight).toBeGreaterThan(listMetrics.clientHeight)
  const lastRow = listEl.locator('.latency-row').last()
  await expect(lastRow).not.toBeInViewport()
  await listEl.evaluate((el: HTMLElement) => { el.scrollTop = el.scrollHeight })
  await expect(lastRow).toBeInViewport()
})

test('connection observation shows collector links and node paths', async ({ page }) => {
  const collector = {
    id: 'collector-1', name: 'Shanghai edge', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, connected_agents: 3, pending_records: 1,
    last_seen: '2026-08-13T06:00:00Z', last_sync: '2026-08-13T05:59:00Z', software_version: '1.4.0', heartbeat_rtt_ms: 18.5, heartbeat_rtt_sampled_at: '2026-08-13T06:00:00Z', scopes: [{ type: 'all', value: '' }],
  }
  const path = {
    server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'primary',
    observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z',
    sink: { connected: true, pending_events: 2, last_error: '', ack_through: 11, last_rtt_ms: 12.5, rtt_sampled_at: '2026-08-13T06:00:00Z' },
  }
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([collector])))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list([path])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/latency**', route => fulfillJSON(route, list([{
    kind: 'collector_heartbeat', collector_id: 'collector-1', server_id: 0, server_name: '', node_uuid: '', observer_id: '',
    bucket_start: '2026-08-13T06:00:00Z', min_ms: 16, avg_ms: 18.5, max_ms: 21, count: 4,
  }])))
  await page.goto('/admin/connections')
  await expect(page.getByRole('heading', { name: '连接观察' })).toBeVisible()
  await expect(page.getByText('主从连接').filter({ visible: true })).toBeVisible()
  const collectorGrid = page.locator('.collector-grid').filter({ visible: true })
  await expect(collectorGrid.getByText('Shanghai edge')).toBeVisible()
  await expect(page.getByText('复制中').filter({ visible: true })).toBeVisible()
  await expect(collectorGrid).not.toContainText('待同步记录')
  await expect(page.getByText('18.5 ms').filter({ visible: true }).first()).toBeVisible()
  await expect(page.locator('.collector-grid .rtt-sampled').filter({ visible: true }).first()).toHaveText(/\d{1,2}:\d{2}:\d{2}/)
  await expect(page.getByText('节点连接').filter({ visible: true })).toBeVisible()
  const pathFilters = page.locator('.connections-page .toolbar-filters')
  await expect(pathFilters).toBeVisible()
  await expect(pathFilters.getByRole('combobox')).toHaveCount(4)
  const card = page.locator('.node-card').filter({ visible: true })
  await expect(card.getByText('edge-a')).toBeVisible()
  await expect(card.getByText('12.5 ms').filter({ visible: true }).first()).toBeVisible()
  await expect(page.locator('.node-end-chip .rtt-sampled').filter({ visible: true }).first()).toHaveText(/\d{1,2}:\d{2}:\d{2}/)
  await collectorGrid.getByText('Shanghai edge').click()
  const drawer = page.locator('.el-drawer').filter({ visible: true })
  await expect(drawer.getByText('从端版本').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText('v1.4.0').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText('心跳', { exact: true })).toBeVisible()
  await expect(drawer.getByText('待同步记录').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText('16 ms', { exact: true })).toBeVisible()
  await expect(drawer.getByText('21 ms', { exact: true })).toBeVisible()
})

test('connection observation truncates long path errors until the drawer opens', async ({ page }) => {
  const collectorId = 'collector-2aee9892d4c14e4c8b9a112233445566'
  const lastError = 'rpc error: code = Unavailable desc = connection error: desc = "transport: authentication handshake failed: tls: first record does not look like a TLS handshake"'
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([{
    id: collectorId, name: '盐湖城1212', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, connected_agents: 0, pending_records: 0,
    last_seen: '2026-08-13T06:00:00Z', last_sync: '2026-08-13T05:59:00Z', scopes: [{ type: 'all', value: '' }],
  }])))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list([{
    server_id: 7, server_name: 'LAX-DMIT.PRO', node_uuid: '09090909090909090909090909090909', observer_id: 'primary',
    observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z',
    sink: { connected: false, pending_events: 0, last_error: lastError, ack_through: 0 },
  }])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/latency**', route => fulfillJSON(route, list()))
  await page.goto('/admin/connections')
  const collectorList = page.locator('.collector-grid').filter({ visible: true })
  await expect(collectorList.getByText('盐湖城1212').filter({ visible: true })).toBeVisible()
  await expect(collectorList.getByText('已跟上').filter({ visible: true })).toBeVisible()
  await expect(collectorList.getByText('collector-2aee9892…').filter({ visible: true })).toBeVisible()
  await expect(collectorList).not.toContainText(collectorId)
  const card = page.locator('.node-card').filter({ visible: true })
  await expect(card.getByText('LAX-DMIT.PRO').filter({ visible: true })).toBeVisible()
  await expect(card).not.toContainText(lastError)
  await card.getByRole('button').filter({ hasText: '未连接' }).click()
  const drawer = page.locator('.el-drawer').filter({ visible: true })
  await expect(drawer.getByText(lastError).filter({ visible: true })).toBeVisible()
})

test('connection observation shows node paths as observer chips on node cards', async ({ page }) => {
  const collector = {
    id: 'collector-1', name: 'Shanghai edge', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, connected_agents: 3, pending_records: 1,
    last_seen: '2026-08-13T06:00:00Z', last_sync: '2026-08-13T05:59:00Z', scopes: [{ type: 'all', value: '' }],
  }
  const sinks = {
    up: { connected: true, pending_events: 0, last_error: '', ack_through: 11, last_rtt_ms: 12.5, rtt_sampled_at: '2026-08-13T06:00:00Z' },
    down: { connected: false, pending_events: 0, last_error: '', ack_through: 0 },
    far: { connected: true, pending_events: 0, last_error: '', ack_through: 8, last_rtt_ms: 40, rtt_sampled_at: '2026-08-13T06:00:00Z' },
  }
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([collector])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list([
    { server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'primary', observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z', sink: sinks.up },
    { server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'collector-1', observer_kind: 'collector', observer_name: 'Shanghai edge', assigned: true, last_seen: '2026-08-13T06:00:00Z', sink: sinks.down },
    { server_id: 8, server_name: 'edge-b', node_uuid: '0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a', observer_id: 'primary', observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z', sink: sinks.down },
    { server_id: 8, server_name: 'edge-b', node_uuid: '0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a', observer_id: 'collector-1', observer_kind: 'collector', observer_name: 'Shanghai edge', assigned: true, last_seen: '2026-08-13T06:00:00Z', sink: sinks.far },
  ])))
  await page.goto('/admin/connections')
  const cards = page.locator('.node-card').filter({ visible: true })
  await expect(cards).toHaveCount(2)
  const edgeA = cards.filter({ hasText: 'edge-a' })
  const edgeB = cards.filter({ hasText: 'edge-b' })
  await expect(edgeA.getByRole('button', { name: /12\.5 ms/ })).toBeVisible()
  await expect(edgeA.getByRole('button', { name: /未连接/ })).toBeVisible()
  await expect(edgeB.getByRole('button', { name: /40 ms/ })).toBeVisible()
  await expect(edgeB.getByRole('button', { name: /未连接/ })).toBeVisible()
  await expect(page.locator('.node-end-chip .rtt-sampled')).toHaveCount(2)
  await expect(page.locator('.node-end-chip').filter({ hasText: '主面板' })).toHaveCount(2)
  await expect(page.locator('.node-end-chip').filter({ hasText: 'Shanghai edge' })).toHaveCount(2)
  const stacked = await edgeA.locator('.node-end-chip').evaluateAll((chips) => {
    if (chips.length < 2) return { stacked: false, reason: 'need 2 chips' }
    const a = chips[0].getBoundingClientRect()
    const b = chips[1].getBoundingClientRect()
    return {
      stacked: b.top >= a.bottom - 1 && Math.abs(a.left - b.left) < 2,
      a: { top: a.top, bottom: a.bottom, left: a.left, width: a.width },
      b: { top: b.top, bottom: b.bottom, left: b.left, width: b.width },
    }
  })
  expect(stacked.stacked, JSON.stringify(stacked)).toBe(true)
})

test('connection node cards stay within the page and scroll internally', async ({ page }) => {
  const servers = Array.from({ length: 40 }, (_, index) => ({
    id: index + 1,
    name: `srv-${String(index + 1).padStart(2, '0')}`,
    node_uuid: `uuid-${index + 1}`,
  }))
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([{
    id: 'collector-1', name: 'Singapore', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, connected_agents: 1, pending_records: 0,
    last_seen: '2026-08-17T06:00:00Z', last_sync: '2026-08-17T06:00:00Z', scopes: [{ type: 'all', value: '' }],
  }])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list(servers.flatMap((server) => ([
    {
      server_id: server.id, server_name: server.name, node_uuid: server.node_uuid, observer_id: 'primary',
      observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-17T06:00:00Z',
      sink: { connected: true, pending_events: 0, last_error: '', ack_through: 1, last_rtt_ms: 12, rtt_sampled_at: '2026-08-17T06:00:00Z' },
    },
    {
      server_id: server.id, server_name: server.name, node_uuid: server.node_uuid, observer_id: 'collector-1',
      observer_kind: 'collector', observer_name: 'Singapore', assigned: true, last_seen: '2026-08-17T06:00:00Z',
      sink: { connected: true, pending_events: 0, last_error: '', ack_through: 1, last_rtt_ms: 20, rtt_sampled_at: '2026-08-17T06:00:00Z' },
    },
  ])))))
  await page.goto('/admin/connections')
  const wrap = page.locator('.node-card-grid-wrap')
  await expect(page.locator('.node-card').first()).toBeVisible()
  await expect(page.getByText('srv-40').first()).toBeAttached()
  await assertAdminContentDoesNotScroll(page)
  const metrics = await wrap.evaluate((el: HTMLElement) => ({
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }))
  expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight)
})

test('connection observation hides stale RTT when collector is offline', async ({ page }) => {
  const observer = {
    id: 'collector-1', name: 'Frankfurt edge', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'offline', revoked: false, kind: 'observer', connected_agents: 0, pending_records: 0,
    last_seen: '2026-08-13T05:00:00Z', last_sync: '2026-08-13T05:00:00Z', heartbeat_rtt_ms: 18.5,
    heartbeat_rtt_sampled_at: '2026-08-13T05:00:00Z', scopes: [{ type: 'all', value: '' }],
  }
  const probe = {
    id: 'collector-2', name: 'SLC probe', address: 'probe.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'offline', revoked: false, kind: 'probe', connected_agents: 0, pending_records: 0,
    last_seen: '2026-08-13T05:00:00Z', last_sync: '2026-08-13T05:00:00Z', heartbeat_rtt_ms: 9,
    heartbeat_rtt_sampled_at: '2026-08-13T05:00:00Z', scopes: [{ type: 'all', value: '' }],
  }
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([observer, probe])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list([{
    server_id: 7, server_name: 'edge-a', collector_id: 'collector-2', collector_name: 'SLC probe',
    target: { source: 'host', ipv4: '192.0.2.10' }, reachable: true, display_rtt_ms: 21.5, sampled_at: '2026-08-13T06:00:00Z',
  }])))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list([
    {
      server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'primary',
      observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z',
      sink: { connected: true, pending_events: 0, last_error: '', ack_through: 11, last_rtt_ms: 12.5, rtt_sampled_at: '2026-08-13T06:00:00Z' },
    },
    {
      server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'collector-1',
      observer_kind: 'collector', observer_name: 'Frankfurt edge', assigned: true, last_seen: '2026-08-13T06:00:00Z',
      sink: { connected: true, pending_events: 0, last_error: '', ack_through: 8, last_rtt_ms: 99.9, rtt_sampled_at: '2026-08-13T06:00:00Z' },
    },
  ])))
  await page.goto('/admin/connections')
  const collectorGrid = page.locator('.collector-grid').filter({ visible: true })
  await expect(collectorGrid.getByText('Frankfurt edge')).toBeVisible()
  await expect(collectorGrid).not.toContainText('SLC probe')
  await expect(collectorGrid.getByText('离线').filter({ visible: true })).toHaveCount(1)
  await expect(collectorGrid).not.toContainText('18.5 ms')
  await expect(collectorGrid).not.toContainText('9 ms')
  const cards = page.locator('.node-card').filter({ visible: true })
  await expect(cards.getByRole('button', { name: /12\.5 ms/ })).toBeVisible()
  await expect(cards.getByRole('button', { name: /未连接/ })).toHaveCount(1)
  await expect(cards).not.toContainText('SLC probe')
  await expect(cards).not.toContainText('99.9 ms')
  await expect(cards).not.toContainText('21.5 ms')
})

test('probe observation shows ICMP paths separately from node links', async ({ page }) => {
  const observer = {
    id: 'collector-1', name: 'Frankfurt edge', address: 'collector.example.com:5555', tls: true, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, kind: 'observer', connected_agents: 1, pending_records: 0,
    last_seen: '2026-08-13T06:00:00Z', scopes: [{ type: 'all', value: '' }],
  }
  const probe = {
    id: 'collector-2', name: 'SLC probe', address: '', tls: false, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, kind: 'probe',
    last_seen: '2026-08-13T06:00:00Z', heartbeat_rtt_ms: 9, heartbeat_rtt_sampled_at: '2026-08-13T06:00:00Z',
    software_version: '1.4.0', scopes: [{ type: 'all', value: '' }],
  }
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([observer, probe])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list([{
    server_id: 7, server_name: 'edge-a', collector_id: 'collector-2', collector_name: 'SLC probe',
    target: { source: 'host', ipv4: '192.0.2.10' }, reachable: true, display_rtt_ms: 21.5, sampled_at: '2026-08-13T06:00:00Z',
    icmp: { ok: true, rtt_ms: 21.5, loss: 0 }, tcp: [{ port: 443, ok: true, rtt_ms: 30 }], mtr: { loss: 0, hop_count: 2 }, has_trace: true,
  }])))
  await page.route('**/api/v2/admin/probes/samples**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/probes/trace**', route => fulfillJSON(route, item(null)))
  await page.goto('/admin/probes')
  await expect(page.getByRole('heading', { name: '探针观察' })).toBeVisible()
  const group = page.locator('.probe-group').filter({ visible: true })
  await expect(group.locator('.collector-tile').getByText('SLC probe')).toBeVisible()
  await expect(group).not.toContainText('Frankfurt edge')
  await expect(group.locator('.collector-tile').getByText('9 ms').filter({ visible: true })).toBeVisible()
  const card = page.locator('.probe-card').filter({ visible: true })
  await expect(card.getByText('edge-a')).toBeVisible()
  await expect(card.getByText('ICMP')).toBeVisible()
  await expect(card.getByText(/TCP/)).toBeVisible()
  await expect(card.getByText('MTR')).toBeVisible()
  await expect(card.getByText(/21\.5 ms/)).toBeVisible()
  await expect(card.getByText(/30 ms/)).toBeVisible()
  await expect(card.getByText('0%')).toBeVisible()
  await expect(page.locator('.path-matrix')).toHaveCount(0)
  await card.click()
  const dialog = page.locator('.el-dialog').filter({ visible: true })
  await expect(dialog.getByText('ICMP').filter({ visible: true }).first()).toBeVisible()
  await expect(page.locator('.el-drawer').filter({ visible: true })).toHaveCount(0)
})

test('probe observation hides closed ICMP and shows TCP MTR hop geo', async ({ page }) => {
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list([{
    id: 'collector-2', name: 'SLC probe', address: '', tls: false, insecure_tls: false,
    generation: 1, config_version: 1, status: 'online', revoked: false, kind: 'probe',
    last_seen: '2026-08-13T06:00:00Z', heartbeat_rtt_ms: 9, heartbeat_rtt_sampled_at: '2026-08-13T06:00:00Z',
    software_version: '1.4.0', scopes: [{ type: 'all', value: '' }],
  }])))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list([{
    server_id: 8, server_name: 'SJC-SALTYFISH.P1', collector_id: 'collector-2', collector_name: 'SLC probe',
    target: { source: 'host', ipv4: '192.0.2.88' }, reachable: true, display_rtt_ms: 182.2, sampled_at: '2026-08-13T06:00:00Z',
    tcp: [{ port: 58880, ok: true, rtt_ms: 182.2 }], mtr: { loss: 0, hop_count: 2, protocol: 'tcp', port: 58880 }, has_trace: true,
  }])))
  await page.route('**/api/v2/admin/probes/samples**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/probes/trace**', route => fulfillJSON(route, item({
    collector_id: 'collector-2', server_id: 8, sampled_at: '2026-08-13T06:00:00Z', destination: '192.0.2.88',
    protocol: 'tcp', port: 58880,
    hops: [
      { ttl: 1, address: '10.0.0.1', avg_ms: 1, loss: 0, sent: 3, private: true },
      { ttl: 8, address: '192.0.2.88', avg_ms: 182.2, loss: 0, sent: 3, geo: 'United States', country_code: 'us' },
    ],
    tcp: {
      sampled_at: '2026-08-13T06:00:00Z', destination: '192.0.2.88', port: 58880,
      hops: [
        { ttl: 1, address: '10.0.0.1', avg_ms: 1, loss: 0, sent: 3, private: true },
        { ttl: 8, address: '192.0.2.88', avg_ms: 182.2, loss: 0, sent: 3, geo: 'United States', country_code: 'us' },
      ],
    },
  })))
  await page.goto('/admin/probes')
  const card = page.locator('.probe-card').filter({ visible: true })
  await expect(card.getByText('SJC-SALTYFISH.P1')).toBeVisible()
  await expect(card.getByText('ICMP')).toBeVisible()
  await expect(card).not.toContainText('超时')
  await expect(card.getByText(/182\.2 ms/)).toBeVisible()
  await expect(card.getByText('0%')).toBeVisible()
  await card.click()
  const dialog = page.locator('.el-dialog').filter({ visible: true })
  await expect(dialog.getByText('超时')).toHaveCount(0)
  await dialog.getByRole('tab', { name: '路径' }).click()
  await expect(dialog.locator('.probe-hop__geo').filter({ hasText: '内网' })).toBeVisible()
  await expect(dialog.getByText('United States')).toBeVisible()
  await expect(dialog.locator('.probe-route-switch')).toBeVisible()
})

test('connection observation refreshes node chip latency on poll', async ({ page }) => {
  await page.clock.install()
  const path = {
    server_id: 7, server_name: 'edge-a', node_uuid: '09090909090909090909090909090909', observer_id: 'primary',
    observer_kind: 'primary', observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z',
    sink: { connected: true, pending_events: 0, last_error: '', ack_through: 11, last_rtt_ms: 12.5, rtt_sampled_at: '2026-08-13T06:00:00Z' },
  }
  let hits = 0
  await page.route('**/api/v2/admin/telemetry/collectors**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/probes/paths**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/paths**', route => {
    hits += 1
    return fulfillJSON(route, list([{
      ...path,
      sink: { ...path.sink, last_rtt_ms: hits === 1 ? 12.5 : 88 },
    }]))
  })
  await page.goto('/admin/connections')
  const chips = page.locator('.node-end-chip').filter({ visible: true })
  await expect(chips.filter({ hasText: /12\.5 ms/ })).toBeVisible()
  await page.clock.fastForward(5000)
  await expect(chips.filter({ hasText: /88 ms/ })).toBeVisible()
})

test('servers list scrolls table body on desktop without moving the page', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'admin-mobile', 'table body lock is desktop-only')
  const servers = Array.from({ length: 20 }, (_, index) => ({
    id: index + 1,
    name: `host-${String(index + 1).padStart(2, '0')}`,
    tag: 'edge',
    display_index: 2000 - index,
    online: true,
    last_active: '2026-08-14T03:36:00Z',
    host: { Platform: 'debian', Version: '1.0.0' },
    public_note: {},
    telemetry: { available: true, coverage: '1/1' },
  }))
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list(servers)))
  await page.goto('/admin/servers')
  await expect(page.getByRole('heading', { name: '主机管理' })).toBeVisible()
  await expect(page.locator('.servers-page .toolbar .search-input')).toBeVisible()
  await expect(page.locator('.servers-page .pagination .el-pagination').filter({ visible: true })).toBeVisible()
  await assertAdminContentDoesNotScroll(page)
  expect(await page.evaluate(() => window.scrollY)).toBe(0)
  expect(await page.locator('.admin-topbar').evaluate((el: HTMLElement) => el.getBoundingClientRect().top)).toBeLessThan(2)

  const lastRow = page.locator('.servers-page .el-table__row').filter({ hasText: 'host-20' })
  await expect(lastRow).toBeAttached()
  await expect(lastRow).not.toBeInViewport()

  const scrolled = await page.locator('.servers-page .el-table').evaluate((table: HTMLElement) => {
    const wrap = (table.querySelector('.el-scrollbar__wrap') || table.querySelector('.el-table__body-wrapper')) as HTMLElement | null
    if (!wrap) return { ok: false, scrollHeight: 0, clientHeight: 0, scrollTop: 0 }
    wrap.scrollTop = wrap.scrollHeight
    return {
      ok: wrap.scrollHeight > wrap.clientHeight,
      scrollHeight: wrap.scrollHeight,
      clientHeight: wrap.clientHeight,
      scrollTop: wrap.scrollTop,
    }
  })
  expect(scrolled.ok, JSON.stringify(scrolled)).toBe(true)
  expect(scrolled.scrollTop).toBeGreaterThan(0)
  await expect(lastRow).toBeInViewport()
  await expect(page.getByRole('heading', { name: '主机管理' })).toBeInViewport()
  await expect(page.locator('.servers-page .toolbar .search-input')).toBeInViewport()
  await expect(page.locator('.servers-page .pagination .el-pagination').filter({ visible: true })).toBeInViewport()
  expect(await page.evaluate(() => window.scrollY)).toBe(0)
})

test('servers list shows reported agent version', async ({ page }) => {
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list([{
    id: 7, name: 'edge-a', tag: 'edge', online: true, last_active: '2026-08-14T03:36:00Z',
    host: { Platform: 'debian', Version: '1.0.0' }, public_note: {}, telemetry: { available: true, coverage: '1/1' },
  }])))
  await page.goto('/admin/servers')
  await expect(page.getByText('探针版本').filter({ visible: true }).first()).toBeVisible()
  await expect(page.getByText('系统', { exact: true }).filter({ visible: true }).first()).toBeVisible()
  await expect(page.getByText('1.0.0').filter({ visible: true }).first()).toBeVisible()
})

test('server history drawer shows observer evidence and connection paths', async ({ page }) => {
  await page.route('**/api/v2/admin/servers**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/availability')) {
      return fulfillJSON(route, list([
        {
          bucket_start: '2026-08-13T06:00:00Z', host: 'online', connectivity: 'full',
          expected_observers: 1, healthy_observers: 1, seen_observers: 1,
          observer_evidence: [{ observer_id: 'primary', observer_kind: 'primary', healthy: true, seen: true }],
          revision: 1, finalized: true,
        },
        {
          bucket_start: '2026-08-13T06:00:30Z', host: 'online', connectivity: 'full',
          expected_observers: 1, healthy_observers: 1, seen_observers: 1,
          observer_evidence: [{ observer_id: 'primary', observer_kind: 'primary', healthy: true, seen: true }],
          revision: 1, finalized: true,
        },
        {
          bucket_start: '2026-08-13T06:01:00Z', host: 'online', connectivity: 'full',
          expected_observers: 1, healthy_observers: 1, seen_observers: 1,
          observer_evidence: [{ observer_id: 'primary', observer_kind: 'primary', healthy: true, seen: true }],
          revision: 1, finalized: true,
        },
        {
          bucket_start: '2026-08-13T06:02:00Z', host: 'online', connectivity: 'full',
          expected_observers: 1, healthy_observers: 1, seen_observers: 1,
          observer_evidence: [{ observer_id: 'primary', observer_kind: 'primary', healthy: true, seen: true }],
          revision: 1, finalized: true,
        },
      ]))
    }
    return fulfillJSON(route, list([{
      id: 7, name: 'edge-a', tag: 'edge', online: true, public_note: {}, monitoring_options: {},
      display_index: 1, hide_for_guest: false, enable_ddns: false,
      telemetry: { host: 'online', connectivity: 'full', available: true, coverage: '2/2' },
    }]))
  })
  await page.route('**/api/v2/admin/offline-history**', route => fulfillJSON(route, list()))
  await page.route('**/api/v2/admin/connections/paths**', route => fulfillJSON(route, list([{
    server_id: 7, server_name: 'edge-a', node_uuid: 'aa', observer_id: 'primary', observer_kind: 'primary',
    observer_name: '', assigned: true, last_seen: '2026-08-13T06:00:00Z',
    sink: { connected: true, pending_events: 0, last_rtt_ms: 9, rtt_sampled_at: '2026-08-13T06:00:00Z' },
  }])))
  await page.goto('/admin/servers')
  await expect(page.locator('.availability-entry').filter({ visible: true }).first()).toContainText('2/2')
  await page.locator('.availability-entry').filter({ visible: true }).first().click()
  const drawer = page.locator('.el-drawer').filter({ visible: true })
  await expect(drawer.getByText('详情 · edge-a')).toBeVisible()
  await expect(drawer.getByRole('tab', { name: '流量' })).toHaveCount(0)
  await expect(drawer.getByText('观测点').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText('主面板').filter({ visible: true }).first()).toBeVisible()
  await expect(drawer.getByText('可用率').filter({ visible: true })).toBeVisible()
  await expect(drawer.locator('.availability-history-table .el-table__body .el-table__row')).toHaveCount(3)
  await page.getByRole('tab', { name: '节点连接' }).click()
  await expect(page.getByText('已连接').filter({ visible: true })).toBeVisible()
  await expect(page.getByText('9 ms').filter({ visible: true })).toBeVisible()
})

test('servers list shows traffic usage and hourly plus daily history', async ({ page }) => {
  let historyTz = ''
  const hourly = Array.from({ length: 24 }, (_, index) => ({
    window_start: `2026-08-17T${String(index).padStart(2, '0')}:00:00.000Z`,
    bytes: index + 1,
  }))
  await page.route('**/api/v2/admin/servers**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/traffic-history')) {
      historyTz = url.searchParams.get('tz') || ''
      return fulfillJSON(route, list([{
        policy_id: 3, server_id: 7, name: 'Monthly total', enabled: true, direction: 'total',
        usage: {
          policy_id: 3, server_id: 7, direction: 'total', mode: 'cumulative',
          window_start: '2026-08-01T00:00:00Z', window_end: null,
          used_bytes: 100, quota_bytes: 1000, warning_percent: 80, usage_percent: 10,
          status: 'normal', updated_at: '2026-08-18T12:00:00Z',
        },
        hourly,
        daily: [
          { window_start: '2026-08-17T00:00:00.000Z', bytes: 80 },
          { window_start: '2026-08-18T00:00:00.000Z', bytes: 45 },
        ],
      }]))
    }
    if (path.endsWith('/availability')) return fulfillJSON(route, list())
    return fulfillJSON(route, list([
      {
        id: 7, name: 'edge-a', tag: 'edge', online: true, public_note: {}, monitoring_options: {},
        display_index: 1, hide_for_guest: false, enable_ddns: false,
        host: { Platform: 'debian', Version: '1.0.0' },
        traffic_summaries: [{ policy_id: 3, name: 'Monthly total', used_bytes: 100, quota_bytes: 1000, usage_percent: 10, status: 'normal' }],
        telemetry: { host: 'online', connectivity: 'full', available: true, coverage: '1/1' },
      },
      {
        id: 8, name: 'edge-b', tag: 'edge', online: true, public_note: {}, monitoring_options: {},
        display_index: 2, hide_for_guest: false, enable_ddns: false,
        traffic_summaries: [],
        state: { NetInTransfer: 400, NetOutTransfer: 600 },
        telemetry: { host: 'online', connectivity: 'full', available: true, coverage: '1/1' },
      },
    ]))
  })
  await page.goto('/admin/servers')
  await expect(page.locator('button.traffic-entry').filter({ visible: true })).toHaveCount(1)
  await expect(page.locator('button.traffic-entry').filter({ visible: true })).toContainText('100 B / 1,000 B')
  await expect(page.locator('span.traffic-entry').filter({ visible: true })).toContainText('1,000 B')
  await expect(page.locator('span.traffic-entry').filter({ visible: true })).not.toContainText('/')
  await page.locator('button.traffic-entry').filter({ visible: true }).click()
  const drawer = page.locator('.el-drawer').filter({ visible: true })
  await expect(drawer.getByText('详情 · edge-a')).toBeVisible()
  await expect(drawer.getByRole('tab', { name: '流量' })).toBeVisible()
  await expect(drawer.getByText('近 24 小时')).toBeVisible()
  await expect(drawer.getByText('按天')).toBeVisible()
  await expect(drawer.locator('.traffic-bars').first().locator('.traffic-bar-slot')).toHaveCount(24)
  await expect(drawer.locator('.traffic-bars').nth(1).locator('.traffic-bar-slot')).toHaveCount(2)
  await expect.poll(() => historyTz).not.toBe('')
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)
  await page.locator('.availability-entry').filter({ visible: true }).nth(1).click()
  const next = page.locator('.el-drawer').filter({ visible: true })
  await expect(next.getByText('详情 · edge-b')).toBeVisible()
  await expect(next.getByRole('tab', { name: '流量' })).toHaveCount(0)
})

test('servers list copies IPv4 and IPv6 from separate columns', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async () => {} },
    })
  })
  await page.route('**/api/v2/admin/servers**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/availability')) return fulfillJSON(route, list())
    return fulfillJSON(route, list([{
      id: 7, name: 'edge-a', tag: 'edge', online: true, last_active: '2026-08-14T03:36:00Z',
      host: { Platform: 'debian', Version: '1.0.0', ipv4: '192.0.2.10', ipv6: '2001:db8::10' },
      public_note: {}, telemetry: { available: true, coverage: '1/1' },
    }]))
  })
  await page.goto('/admin/servers')
  const ipv4 = page.locator('.copyable-text').filter({ hasText: '192.0.2.10' }).filter({ visible: true })
  const ipv6 = page.locator('.copyable-text').filter({ hasText: '2001:db8::10' }).filter({ visible: true })
  await expect(ipv4).toBeVisible()
  await expect(ipv6).toBeVisible()
  await ipv4.click()
  await expect(page.getByText('已复制').filter({ visible: true })).toBeVisible()
})

test('servers list stores page size in localStorage', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name === 'admin-mobile', 'sizes selector is hidden below 600px')
  const sizes: string[] = []
  await page.route('**/api/v2/admin/servers**', route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/servers')) sizes.push(url.searchParams.get('page_size') || '')
    return fulfillJSON(route, list([{
      id: 7, name: 'edge-a', tag: 'edge', online: true, last_active: '2026-08-14T03:36:00Z',
      host: { Platform: 'debian', Version: '1.0.0' }, public_note: {}, telemetry: { available: true, coverage: '1/1' },
    }]))
  })
  await page.goto('/admin/servers')
  const sizesSelect = page.locator('.el-pagination__sizes')
  await sizesSelect.scrollIntoViewIfNeeded()
  await expect(sizesSelect).toBeVisible()
  await sizesSelect.click()
  await page.getByRole('option', { name: /50/ }).click()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('santaizi-admin-page-size:/servers'))).toBe('50')
  await expect.poll(() => sizes.includes('50')).toBeTruthy()
})

test('telemetry datasets show readable rows without blobs or full uuids', async ({ page }) => {
  const nodeUUID = 'dacee892aabbccddeeff001122334455'
  await page.route('**/api/v2/admin/telemetry/agents**', route => fulfillJSON(route, list([{
    server_id: 7, server_name: 'edge-a', node_uuid: nodeUUID, wal_pressure: 'healthy', wal_bytes: 2048,
    pending_events: 2, oldest_pending: '2026-08-13T06:00:00Z', clock_untrusted: false, protocol_version: 'v2',
    updated_at: '2026-08-13T06:01:00Z',
    sinks: [{ endpoint_id: 'primary', observer_kind: 'primary', observer_name: '', connected: true, pending_events: 2, last_error: '', ack_through: 11 }],
  }])))
  await page.route('**/api/v2/admin/telemetry/incidents**', route => fulfillJSON(route, list([{
    id: 3, server_id: 7, server_name: 'edge-a', node_uuid: nodeUUID, initial_classification: 'host_offline',
    current_classification: 'connectivity_degraded', revision: 2, started_at: '2026-08-13T05:00:00Z',
    ended_at: null, reason: 'availability_evidence',
    observer_evidence: [{ observer_id: 'primary', observer_kind: 'primary', observer_name: '', healthy: true, seen: true }],
  }])))

  await page.goto('/admin/telemetry')
  await page.getByRole('tab', { name: '探针状态' }).click()
  const agentsList = page.locator('.dataset-table, .mobile-card-list').filter({ visible: true }).first()
  await expect(agentsList.getByText('edge-a').filter({ visible: true })).toBeVisible()
  await expect(agentsList.getByText('健康').filter({ visible: true })).toBeVisible()
  await expect(agentsList).not.toContainText(nodeUUID)
  await expect(agentsList).not.toContainText('CgkJCQk')
  await agentsList.locator('.el-table__row, .mobile-card').first().click()
  const drawer = page.locator('.el-drawer').filter({ visible: true })
  await expect(drawer.getByText('主面板').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText('已连接').filter({ visible: true })).toBeVisible()
  await expect(drawer.getByText(nodeUUID).filter({ visible: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)

  await page.getByRole('tab', { name: '连通异常', exact: true }).click()
  const incidentsList = page.locator('.dataset-table, .mobile-card-list').filter({ visible: true }).first()
  await expect(incidentsList.getByText('连通性降级').filter({ visible: true })).toBeVisible()
  await expect(incidentsList.getByText('进行中').filter({ visible: true })).toBeVisible()
  await expect(incidentsList).not.toContainText(nodeUUID)
  await expect(incidentsList).not.toContainText('observer_id')
  await incidentsList.locator('.el-table__row, .mobile-card').first().click()
  await expect(page.locator('.el-drawer').filter({ visible: true }).getByText(nodeUUID)).toBeVisible()
})

test('telemetry dataset tabs paginate incident revisions', async ({ page }) => {
  const pages: string[] = []
  await page.route('**/api/v2/admin/telemetry/incident-revisions**', route => {
    const url = new URL(route.request().url())
    pages.push(url.searchParams.get('page') || '1')
    const pageNum = Number(url.searchParams.get('page') || '1')
    const rows = Array.from({ length: pageNum === 1 ? 20 : 5 }, (_, i) => ({
      id: pageNum === 1 ? 268 - i : 248 - i, incident_id: pageNum === 1 ? 268 - i : 248 - i, revision: 1,
      classification: 'host_offline', reason: 'availability_evidence',
      created_at: '2026-08-13T08:19:13Z', recalculated_at: '2026-08-13T08:19:13Z', observer_evidence: [],
    }))
    return fulfillJSON(route, JSON.stringify({ data: rows, meta: { page: pageNum, page_size: 20, total: 45 } }))
  })
  await page.goto('/admin/telemetry')
  await page.getByRole('tab', { name: '修订记录' }).click()
  const pagination = page.locator('.pagination .el-pagination').filter({ visible: true })
  await expect(pagination).toBeVisible()
  await expect(pagination.locator('.el-pagination__total')).toContainText('45')
  await pagination.locator('.btn-next').click()
  await expect.poll(() => pages.includes('2')).toBeTruthy()
})

test('narrow admin lists scroll inside admin-content without moving the topbar', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 667 })
  const servers = [
    { id: 1, name: 'LAX-ALPHA', tag: 'USA', display_index: 2000, online: true, last_active: '2026-08-14T03:36:00Z', host: { Platform: 'debian', Version: '1.0.0', ipv4: '192.0.2.10', ipv6: '2001:db8::10' }, public_note: {}, telemetry: { available: true, coverage: '2/2' } },
    { id: 2, name: 'LAX-BRAVO', tag: 'USA', display_index: 1990, online: true, last_active: '2026-08-14T03:36:00Z', host: { Platform: 'debian', ipv4: '192.0.2.11', ipv6: '2001:db8::11' }, public_note: {}, telemetry: { available: true, coverage: '2/2' } },
    { id: 3, name: 'NRT-CHARLIE', tag: 'JPN', display_index: 1980, online: false, last_active: '2026-08-13T12:00:00Z', host: { Platform: 'ubuntu', ipv4: '192.0.2.12', ipv6: '2001:db8::12' }, public_note: {}, telemetry: { available: false, coverage: '0/2' } },
  ]
  await page.route('**/api/v2/admin/servers**', route => fulfillJSON(route, list(servers)))
  await page.goto('/admin/servers')
  await expect(page.locator('.mobile-card').filter({ visible: true })).toHaveCount(3)
  await assertAdminContentScrolls(page, page.locator('.mobile-card').filter({ visible: true }).last())

  const monitors = Array.from({ length: 6 }, (_, i) => ({
    id: i + 1, name: `probe-${i + 1}`, type: 'http', target: `https://example.test/${i + 1}`, interval_seconds: 30,
    notify: false, notification_tag: '', scope: { mode: 'all', server_ids: [] },
  }))
  await page.route('**/api/v2/admin/monitors**', route => fulfillJSON(route, list(monitors)))
  await page.goto('/admin/services')
  await expect(page.locator('.mobile-card').filter({ visible: true })).toHaveCount(6)
  await assertAdminContentScrolls(page, page.locator('.mobile-card').filter({ visible: true }).last())
})
