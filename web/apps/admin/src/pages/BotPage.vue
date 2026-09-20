<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AppEmpty } from '@santaizi/ui'
import BotChatEditorDialog from '@/components/editors/BotChatEditorDialog.vue'
import BotReportEditorDialog from '@/components/editors/BotReportEditorDialog.vue'
import CopyableText from '@/components/CopyableText.vue'
import {
  createBotBindCode, deleteBotBindCode, deleteBotChat, deleteBotReport,
  getBotSettings, listBotBindCodes, listBotChats, listBotReports,
  runBotReport, testBot, updateBotSettings,
} from '@/api/adminApi'
import type { BotSettingsWrite } from '@santaizi/api'
import { notifyAPIError } from '@/composables/notify'
import { formatDateTime } from '@/composables/format'
import { readStoredPageSize, writeStoredPageSize } from '@/composables/pageSize'
import { isRowSelected, toggleRowSelection } from '@/composables/selection'
import type { BotBindCodeRecord, BotChatRecord, BotReportRecord, BotRole, BotSettings } from '@/types/admin'

const { t, te, locale } = useI18n()
const route = useRoute()
const saving = ref(false), testing = ref(false), creatingCode = ref(false)
const chatEditor = ref(false), reportEditor = ref(false)
const chatsLoading = ref(false), codesLoading = ref(false), reportsLoading = ref(false)
const chats = ref<BotChatRecord[]>([]), codes = ref<BotBindCodeRecord[]>([]), reports = ref<BotReportRecord[]>([])
const selectedChats = ref<BotChatRecord[]>([]), selectedCodes = ref<BotBindCodeRecord[]>([]), selectedReports = ref<BotReportRecord[]>([])
const editingChat = ref<BotChatRecord>(), editingReport = ref<BotReportRecord>()
const chatsTotal = ref(0), codesTotal = ref(0), reportsTotal = ref(0)
const latestCode = ref<BotBindCodeRecord>()
const pageSize = readStoredPageSize(route.path)
const chatQuery = reactive({ page: 1, page_size: pageSize, q: '', sort: 'id', order: 'desc' as const })
const codeQuery = reactive({ page: 1, page_size: pageSize })
const reportQuery = reactive({ page: 1, page_size: pageSize, q: '', sort: 'id', order: 'desc' as const })
const bindForm = reactive<{ role: BotRole; ttl_minutes: number }>({ role: 'viewer', ttl_minutes: 10 })
const settings = reactive<BotSettings & { token: string; webhook_secret: string }>({
  enabled: false, provider: 'telegram', mode: 'polling', api_endpoint: '', webhook_base_url: '',
  charts: false, language: '', rate_per_minute: 20, token_set: false, token_suffix: '', webhook_secret_set: false,
  token: '', webhook_secret: '',
})
const tokenHint = computed(() => settings.token_set && settings.token_suffix ? t('botTokenSet', { suffix: settings.token_suffix }) : t('botTokenUnset'))

function clampNumber(value: unknown, min: number, max: number, fallback: number) {
  const next = Number(value)
  if (!Number.isFinite(next)) return fallback
  return Math.min(max, Math.max(min, next))
}

function roleLabel(role: string) {
  if (role === 'admin') return t('botRoleAdmin')
  if (role === 'operator') return t('botRoleOperator')
  if (role === 'blocked') return t('botRoleBlocked')
  return t('botRoleViewer')
}

function periodLabel(period: string) {
  if (period === 'weekly') return t('botPeriodWeekly')
  if (period === 'monthly') return t('botPeriodMonthly')
  return t('botPeriodDaily')
}

function persistPageSize() {
  writeStoredPageSize(route.path, chatQuery.page_size)
  codeQuery.page_size = chatQuery.page_size
  reportQuery.page_size = chatQuery.page_size
}

async function loadSettings() {
  try {
    const data = await getBotSettings()
    Object.assign(settings, data, { token: '', webhook_secret: '' })
  } catch (error) { notifyAPIError(error, t as never, te) }
}

async function loadChats() {
  persistPageSize()
  chatsLoading.value = true
  try {
    const result = await listBotChats(chatQuery)
    chats.value = result.data
    chatsTotal.value = result.meta.total || result.data.length
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { chatsLoading.value = false }
}

async function loadCodes() {
  persistPageSize()
  codesLoading.value = true
  try {
    const result = await listBotBindCodes(codeQuery)
    codes.value = result.data
    codesTotal.value = result.meta.total || result.data.length
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { codesLoading.value = false }
}

async function loadReports() {
  persistPageSize()
  reportsLoading.value = true
  try {
    const result = await listBotReports(reportQuery)
    reports.value = result.data
    reportsTotal.value = result.meta.total || result.data.length
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { reportsLoading.value = false }
}

async function saveSettings() {
  saving.value = true
  try {
    settings.rate_per_minute = clampNumber(settings.rate_per_minute, 1, 60, 20)
    const payload: BotSettingsWrite = {
      enabled: settings.enabled, provider: settings.provider, mode: settings.mode,
      api_endpoint: settings.api_endpoint, webhook_base_url: settings.webhook_base_url,
      charts: settings.charts, rate_per_minute: settings.rate_per_minute,
    }
    if (settings.token.trim()) payload.token = settings.token.trim()
    if (settings.webhook_secret.trim()) payload.webhook_secret = settings.webhook_secret.trim()
    const data = await updateBotSettings(payload)
    Object.assign(settings, data, { token: '', webhook_secret: '' })
    ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}

async function runTest() {
  testing.value = true
  try {
    const result = await testBot()
    ElMessage.success(t('botTestOk', { name: result.username || 'bot' }))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { testing.value = false }
}

async function createCode() {
  creatingCode.value = true
  try {
    latestCode.value = await createBotBindCode({
      role: bindForm.role,
      ttl_seconds: clampNumber(bindForm.ttl_minutes, 1, 1440, 10) * 60,
    })
    ElMessage.success(t('botBindCodeCreated'))
    await loadCodes()
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { creatingCode.value = false }
}

function openChat(item: BotChatRecord) { editingChat.value = item; chatEditor.value = true }
function openReport(item?: BotReportRecord) { editingReport.value = item; reportEditor.value = true }

async function removeChats(rows: BotChatRecord[]) {
  await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' })
  try {
    await Promise.all(rows.map(row => deleteBotChat(row.id)))
    selectedChats.value = []
    await loadChats()
    ElMessage.success(t('deleteSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
}

async function removeCodes(rows: BotBindCodeRecord[]) {
  await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' })
  try {
    await Promise.all(rows.map(row => deleteBotBindCode(row.id)))
    selectedCodes.value = []
    await loadCodes()
    ElMessage.success(t('deleteSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
}

async function removeReports(rows: BotReportRecord[]) {
  await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' })
  try {
    await Promise.all(rows.map(row => deleteBotReport(row.id)))
    selectedReports.value = []
    await loadReports()
    ElMessage.success(t('deleteSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
}

async function sendReport(row: BotReportRecord) {
  try {
    await runBotReport(row.id)
    ElMessage.success(t('botReportSent'))
    await loadReports()
  } catch (error) { notifyAPIError(error, t as never, te) }
}

function onSelectChat(row: BotChatRecord, checked: boolean | string | number) { selectedChats.value = toggleRowSelection(selectedChats.value, row, !!checked) }
function onSelectCode(row: BotBindCodeRecord, checked: boolean | string | number) { selectedCodes.value = toggleRowSelection(selectedCodes.value, row, !!checked) }
function onSelectReport(row: BotReportRecord, checked: boolean | string | number) { selectedReports.value = toggleRowSelection(selectedReports.value, row, !!checked) }

onMounted(() => { void loadSettings(); void loadChats(); void loadCodes(); void loadReports() })
</script>

<template>
  <div class="page-head">
    <h1>{{ t('botPageTitle') }}</h1>
    <div class="page-actions">
      <el-button :loading="testing" @click="runTest"><i class="ri-link"></i>{{ t('botTest') }}</el-button>
      <el-button type="primary" :loading="saving" @click="saveSettings"><i class="ri-save-line"></i>{{ t('save') }}</el-button>
    </div>
  </div>
  <div class="page-stack">
    <section class="surface settings-section">
      <div class="settings-heading"><i class="ri-robot-2-line"></i><div><h2>{{ t('botConnection') }}</h2></div></div>
      <el-form :model="settings" label-position="top">
        <div class="form-grid">
          <el-form-item :label="t('enabled')"><el-switch v-model="settings.enabled" /></el-form-item>
          <el-form-item :label="t('botCharts')"><el-switch v-model="settings.charts" /></el-form-item>
          <el-form-item class="span-2" :label="t('botToken')">
            <el-input v-model="settings.token" type="password" show-password autocomplete="new-password" :placeholder="tokenHint" />
          </el-form-item>
          <el-form-item :label="t('botMode')">
            <el-segmented v-model="settings.mode" :options="[{ label: t('botModePolling'), value: 'polling' }, { label: t('botModeWebhook'), value: 'webhook' }]" />
          </el-form-item>
          <el-form-item :label="t('botRatePerMinute')">
            <el-input v-model.number="settings.rate_per_minute" inputmode="numeric" class="field-full" @blur="settings.rate_per_minute = clampNumber(settings.rate_per_minute, 1, 60, 20)" />
          </el-form-item>
          <el-form-item class="span-2" :label="t('botAPIEndpoint')"><el-input v-model="settings.api_endpoint" /></el-form-item>
          <template v-if="settings.mode === 'webhook'">
            <el-form-item class="span-2" :label="t('botWebhookBaseURL')"><el-input v-model="settings.webhook_base_url" /></el-form-item>
            <el-form-item class="span-2" :label="t('botWebhookSecret')">
              <el-input v-model="settings.webhook_secret" type="password" show-password autocomplete="new-password" :placeholder="settings.webhook_secret_set ? t('botWebhookSecretSet') : ''" />
            </el-form-item>
          </template>
        </div>
      </el-form>
    </section>

    <section class="surface table-card">
      <div class="settings-heading"><i class="ri-chat-1-line"></i><div><h2>{{ t('botChats') }}</h2></div></div>
      <div class="toolbar">
        <el-input v-model="chatQuery.q" class="search-input" clearable :placeholder="t('search')" @keyup.enter="chatQuery.page=1;loadChats()"><template #prefix><i class="ri-search-line"></i></template></el-input>
        <el-button @click="chatQuery.page=1;loadChats()"><i class="ri-search-line"></i>{{ t('submitSearch') }}</el-button>
        <el-button v-if="selectedChats.length" type="danger" plain @click="removeChats(selectedChats)"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button>
        <span class="toolbar-spacer"></span>
        <el-button @click="loadChats"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button>
      </div>
      <el-table class="desktop-only" v-loading="chatsLoading" :data="chats" row-key="id" @selection-change="selectedChats=$event">
        <el-table-column type="selection" width="46"/>
        <el-table-column prop="title" :label="t('name')" min-width="160"/>
        <el-table-column prop="chat_id" :label="t('id')" width="160"/>
        <el-table-column :label="t('botChatKind')" width="120"><template #default="{row}">{{ row.kind || '—' }}</template></el-table-column>
        <el-table-column :label="t('botRole')" width="120"><template #default="{row}"><el-tag effect="plain">{{ roleLabel(row.role) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('status')" width="100"><template #default="{row}"><el-tag effect="plain">{{ row.enabled ? t('enabled') : t('disabled') }}</el-tag></template></el-table-column>
        <el-table-column :label="t('lastSeen')" width="180"><template #default="{row}">{{ formatDateTime(row.last_seen_at, locale) }}</template></el-table-column>
        <el-table-column :label="t('actions')" width="72" fixed="right">
          <template #default="{row}">
            <el-dropdown trigger="click">
              <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="openChat(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                  <el-dropdown-item divided @click="removeChats([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
        <template #empty><AppEmpty icon="ri-chat-1-line" :description="t('noData')"/></template>
      </el-table>
      <div class="mobile-only" v-loading="chatsLoading">
        <AppEmpty v-if="!chats.length && !chatsLoading" icon="ri-chat-1-line" :description="t('noData')"/>
        <div v-else class="mobile-card-list">
          <article v-for="row in chats" :key="row.id" class="mobile-card">
            <div class="mobile-card-head">
              <el-checkbox :model-value="isRowSelected(selectedChats, row)" @change="onSelectChat(row, $event)" />
              <div class="mobile-card-title"><strong>{{ row.title || row.chat_id }}</strong></div>
              <div class="mobile-card-actions">
                <el-dropdown trigger="click">
                  <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item @click="openChat(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                      <el-dropdown-item divided @click="removeChats([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </div>
            <dl class="mobile-card-meta">
              <div><dt>{{ t('botRole') }}</dt><dd>{{ roleLabel(row.role) }}</dd></div>
              <div><dt>{{ t('status') }}</dt><dd>{{ row.enabled ? t('enabled') : t('disabled') }}</dd></div>
            </dl>
          </article>
        </div>
      </div>
      <div class="pagination"><el-pagination v-model:current-page="chatQuery.page" v-model:page-size="chatQuery.page_size" layout="total, sizes, prev, pager, next" :total="chatsTotal" @change="loadChats"/></div>
    </section>

    <section class="surface table-card">
      <div class="settings-heading"><i class="ri-key-2-line"></i><div><h2>{{ t('botBindCodes') }}</h2></div></div>
      <el-alert type="warning" :closable="false" show-icon :title="t('botGroupRoleWarning')" style="margin-bottom: 16px" />
      <div class="toolbar">
        <el-select v-model="bindForm.role" style="width:140px">
          <el-option :label="t('botRoleViewer')" value="viewer" />
          <el-option :label="t('botRoleOperator')" value="operator" />
          <el-option :label="t('botRoleAdmin')" value="admin" />
        </el-select>
        <el-input v-model.number="bindForm.ttl_minutes" inputmode="numeric" style="width:140px" :placeholder="t('botBindTTL')" @blur="bindForm.ttl_minutes = clampNumber(bindForm.ttl_minutes, 1, 1440, 10)" />
        <el-button type="primary" :loading="creatingCode" @click="createCode"><i class="ri-add-line"></i>{{ t('botCreateBindCode') }}</el-button>
        <el-button v-if="selectedCodes.length" type="danger" plain @click="removeCodes(selectedCodes)"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button>
        <span class="toolbar-spacer"></span>
        <el-button @click="loadCodes"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button>
      </div>
      <p v-if="latestCode" class="bind-code-result"><CopyableText :value="`/bind ${latestCode.code}`" /></p>
      <el-table class="desktop-only" v-loading="codesLoading" :data="codes" row-key="id" @selection-change="selectedCodes=$event">
        <el-table-column type="selection" width="46"/>
        <el-table-column prop="code" :label="t('token')" min-width="180"><template #default="{row}"><CopyableText :value="row.code" /></template></el-table-column>
        <el-table-column :label="t('botRole')" width="120"><template #default="{row}">{{ roleLabel(row.role) }}</template></el-table-column>
        <el-table-column :label="t('status')" width="120"><template #default="{row}">{{ row.used_at ? t('botBindUsed') : t('botBindUnused') }}</template></el-table-column>
        <el-table-column :label="t('tokenExpiresAt')" width="180"><template #default="{row}">{{ formatDateTime(row.expires_at, locale) }}</template></el-table-column>
        <el-table-column :label="t('actions')" width="72" fixed="right">
          <template #default="{row}">
            <el-button text class="actions-more" :aria-label="t('delete')" @click="removeCodes([row])"><i class="ri-delete-bin-6-line"></i></el-button>
          </template>
        </el-table-column>
        <template #empty><AppEmpty icon="ri-key-2-line" :description="t('noData')"/></template>
      </el-table>
      <div class="mobile-only" v-loading="codesLoading">
        <AppEmpty v-if="!codes.length && !codesLoading" icon="ri-key-2-line" :description="t('noData')"/>
        <div v-else class="mobile-card-list">
          <article v-for="row in codes" :key="row.id" class="mobile-card">
            <div class="mobile-card-head">
              <el-checkbox :model-value="isRowSelected(selectedCodes, row)" @change="onSelectCode(row, $event)" />
              <div class="mobile-card-title"><CopyableText :value="row.code" /></div>
              <div class="mobile-card-actions"><el-button text :aria-label="t('delete')" @click="removeCodes([row])"><i class="ri-delete-bin-6-line"></i></el-button></div>
            </div>
            <dl class="mobile-card-meta">
              <div><dt>{{ t('botRole') }}</dt><dd>{{ roleLabel(row.role) }}</dd></div>
              <div><dt>{{ t('status') }}</dt><dd>{{ row.used_at ? t('botBindUsed') : t('botBindUnused') }}</dd></div>
            </dl>
          </article>
        </div>
      </div>
      <div class="pagination"><el-pagination v-model:current-page="codeQuery.page" v-model:page-size="codeQuery.page_size" layout="total, sizes, prev, pager, next" :total="codesTotal" @change="loadCodes"/></div>
    </section>

    <section class="surface table-card">
      <div class="settings-heading">
        <i class="ri-calendar-schedule-line"></i>
        <div><h2>{{ t('botReports') }}</h2></div>
        <el-button type="primary" @click="openReport()"><i class="ri-add-line"></i>{{ t('createBotReport') }}</el-button>
      </div>
      <div class="toolbar">
        <el-input v-model="reportQuery.q" class="search-input" clearable :placeholder="t('search')" @keyup.enter="reportQuery.page=1;loadReports()"><template #prefix><i class="ri-search-line"></i></template></el-input>
        <el-button @click="reportQuery.page=1;loadReports()"><i class="ri-search-line"></i>{{ t('submitSearch') }}</el-button>
        <el-button v-if="selectedReports.length" type="danger" plain @click="removeReports(selectedReports)"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button>
        <span class="toolbar-spacer"></span>
        <el-button @click="loadReports"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button>
      </div>
      <el-table class="desktop-only" v-loading="reportsLoading" :data="reports" row-key="id" @selection-change="selectedReports=$event">
        <el-table-column type="selection" width="46"/>
        <el-table-column prop="name" :label="t('name')" min-width="160"/>
        <el-table-column :label="t('botPeriod')" width="120"><template #default="{row}">{{ periodLabel(row.period) }}</template></el-table-column>
        <el-table-column :label="t('status')" width="100"><template #default="{row}">{{ row.enabled ? t('enabled') : t('disabled') }}</template></el-table-column>
        <el-table-column :label="t('botLastRun')" width="180"><template #default="{row}">{{ formatDateTime(row.last_run_at, locale) }}</template></el-table-column>
        <el-table-column :label="t('botLastStatus')" min-width="140"><template #default="{row}">{{ row.last_status || '—' }}</template></el-table-column>
        <el-table-column :label="t('actions')" width="72" fixed="right">
          <template #default="{row}">
            <el-dropdown trigger="click">
              <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="openReport(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                  <el-dropdown-item @click="sendReport(row)"><i class="ri-send-plane-line"></i>{{ t('botRunNow') }}</el-dropdown-item>
                  <el-dropdown-item divided @click="removeReports([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
        <template #empty><AppEmpty icon="ri-calendar-schedule-line" :description="t('noData')"/></template>
      </el-table>
      <div class="mobile-only" v-loading="reportsLoading">
        <AppEmpty v-if="!reports.length && !reportsLoading" icon="ri-calendar-schedule-line" :description="t('noData')"/>
        <div v-else class="mobile-card-list">
          <article v-for="row in reports" :key="row.id" class="mobile-card">
            <div class="mobile-card-head">
              <el-checkbox :model-value="isRowSelected(selectedReports, row)" @change="onSelectReport(row, $event)" />
              <div class="mobile-card-title"><strong>{{ row.name }}</strong></div>
              <div class="mobile-card-actions">
                <el-dropdown trigger="click">
                  <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item @click="openReport(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                      <el-dropdown-item @click="sendReport(row)"><i class="ri-send-plane-line"></i>{{ t('botRunNow') }}</el-dropdown-item>
                      <el-dropdown-item divided @click="removeReports([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </div>
            <dl class="mobile-card-meta">
              <div><dt>{{ t('botPeriod') }}</dt><dd>{{ periodLabel(row.period) }}</dd></div>
              <div><dt>{{ t('status') }}</dt><dd>{{ row.enabled ? t('enabled') : t('disabled') }}</dd></div>
            </dl>
          </article>
        </div>
      </div>
      <div class="pagination"><el-pagination v-model:current-page="reportQuery.page" v-model:page-size="reportQuery.page_size" layout="total, sizes, prev, pager, next" :total="reportsTotal" @change="loadReports"/></div>
    </section>
  </div>
  <BotChatEditorDialog v-model="chatEditor" :value="editingChat" @saved="loadChats"/>
  <BotReportEditorDialog v-model="reportEditor" :value="editingReport" @saved="loadReports"/>
</template>

<style scoped>
.bind-code-result { margin: 0 0 12px; font-size: 13px; }
</style>
