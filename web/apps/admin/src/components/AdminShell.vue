<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { AppDrawer, AppEmpty } from '@santaizi/ui'
import { useSessionStore } from '@/stores/session'
import { useMessageStore, type AdminMessage } from '@/stores/messages'
import { useTheme } from '@/composables/theme'
import { formatDateTime, formatProductVersion } from '@/composables/format'
import ScriptCommandsDrawer from '@/components/ScriptCommandsDrawer.vue'

const { t, locale } = useI18n()
const session = useSessionStore()
const messages = useMessageStore()
const router = useRouter()
const collapsed = ref(localStorage.getItem('santaizi-admin-collapsed') === '1')
const addonsOpen = ref(localStorage.getItem('santaizi-admin-addons-open') === '1')
const mobileOpen = ref(false)
const scriptsOpen = ref(false)
const { setMode } = useTheme()
const nav = computed(() => [
  ['overview', '/', 'ri-dashboard-line', t('overview')], ['servers', '/servers', 'ri-server-line', t('servers')],
  ['services', '/services', 'ri-pulse-line', t('services')], ['notifications', '/notifications', 'ri-notification-3-line', t('notificationChannels')],
  ['alert-rules', '/alert-rules', 'ri-alarm-warning-line', t('alertRules')], ['telemetry', '/telemetry', 'ri-base-station-line', t('telemetry')],
  ['connections', '/connections', 'ri-links-line', t('connections')],
  ['probes', '/probes', 'ri-radar-line', t('probeObservation')],
  ['settings', '/settings', 'ri-settings-3-line', t('settings')], ['api-tokens', '/api-tokens', 'ri-key-2-line', t('apiTokens')],
])
const addonNav = computed(() => [['ddns', '/ddns', 'ri-global-line', t('ddns')], ['nat', '/nat', 'ri-route-line', t('nat')], ['bot', '/bot', 'ri-robot-2-line', t('bot')]])
const panelVersion = computed(() => formatProductVersion(session.state.version))
const activeMessage = computed(() => messages.activeMessage)
const fieldEntries = computed(() => Object.entries(activeMessage.value?.fields || {}))
const drawerTitle = computed(() => activeMessage.value ? t('errorDetail') : t('messageCenter'))
function toggle() { collapsed.value = !collapsed.value; localStorage.setItem('santaizi-admin-collapsed', collapsed.value ? '1' : '0') }
function toggleAddons() { addonsOpen.value = !addonsOpen.value; localStorage.setItem('santaizi-admin-addons-open', addonsOpen.value ? '1' : '0') }
function setLocale(value: string) { locale.value = value; localStorage.setItem('santaizi-locale', value) }
function goHome() { location.assign('/') }
function onPanelOpen(value: boolean) { if (!value) messages.closePanel() }
async function clearAll() {
  await ElMessageBox.confirm(t('clearMessagesConfirm'), t('confirm'), { type: 'warning' })
  messages.clear()
}
function openItem(row: AdminMessage) { messages.openDetail(row.id) }
</script>

<template>
  <div class="admin-layout" :class="{ collapsed }">
    <aside class="admin-sidebar" :class="{ open: mobileOpen }">
      <a class="brand" href="/"><img :src="'/static/logo.svg'" alt=""><span v-if="!collapsed">{{ t('appName') }}</span></a>
      <nav :aria-label="t('adminNavigation')">
        <RouterLink v-for="item in nav" :key="item[0]" :to="String(item[1])" class="nav-item" :title="collapsed ? String(item[3]) : undefined" @click="mobileOpen=false">
          <i :class="item[2]" aria-hidden="true"></i>
          <span v-if="!collapsed">{{ item[3] }}</span>
        </RouterLink>
        <el-button text class="addon-toggle" :class="{ active: addonNav.some(item => router.currentRoute.value.path === item[1]) }" :aria-expanded="addonsOpen" @click="toggleAddons"><i class="ri-apps-2-line"></i><span v-if="!collapsed">{{ t('additionalFeatures') }}</span><i v-if="!collapsed" :class="addonsOpen ? 'ri-arrow-up-s-line' : 'ri-arrow-down-s-line'"></i></el-button>
        <div v-if="addonsOpen && !collapsed" class="addon-nav"><RouterLink v-for="item in addonNav" :key="item[0]" :to="String(item[1])" class="nav-item" @click="mobileOpen=false"><i :class="item[2]"></i><span>{{ item[3] }}</span></RouterLink></div>
      </nav>
      <div class="sidebar-footer">
        <span v-if="panelVersion" class="sidebar-version" :title="panelVersion">{{ panelVersion }}</span>
      </div>
    </aside>
    <div class="admin-body">
      <header class="admin-topbar">
        <el-button class="mobile-menu" @click="mobileOpen=!mobileOpen" :aria-label="t('openNavigation')"><i class="ri-menu-line"></i></el-button>
        <el-button class="topbar-icon collapse-button" @click="toggle" :aria-expanded="!collapsed" :aria-label="collapsed ? t('expand') : t('collapse')">
          <i :class="collapsed ? 'ri-arrow-right-double-line' : 'ri-arrow-left-double-line'"></i>
        </el-button>
        <div class="topbar-spacer"></div>
        <el-button class="topbar-icon" :aria-label="t('scriptCommands')" @click="scriptsOpen = true"><i class="ri-terminal-box-line"></i></el-button>
        <el-badge :value="messages.unreadCount" :hidden="!messages.unreadCount" :max="99" class="topbar-badge">
          <el-button class="topbar-icon" :aria-label="t('messageCenter')" @click="messages.openPanel()"><i class="ri-notification-badge-line"></i></el-button>
        </el-badge>
        <el-dropdown trigger="click" @command="setLocale">
          <el-button class="topbar-icon" :aria-label="t('language')"><i class="ri-translate-2"></i></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="zh-CN">简体中文</el-dropdown-item>
              <el-dropdown-item command="zh-TW">繁體中文</el-dropdown-item>
              <el-dropdown-item command="en-US">English</el-dropdown-item>
              <el-dropdown-item command="es-ES">Español</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown trigger="click" @command="setMode">
          <el-button class="topbar-icon" :aria-label="t('light')"><i class="ri-contrast-2-line"></i></el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="system"><i class="ri-computer-line"></i>{{ t('system') }}</el-dropdown-item><el-dropdown-item command="light"><i class="ri-sun-line"></i>{{ t('light') }}</el-dropdown-item><el-dropdown-item command="dark"><i class="ri-moon-line"></i>{{ t('dark') }}</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
        <el-dropdown trigger="click">
          <el-button class="user-button" :aria-label="session.state.user?.name || session.state.user?.login || 'admin'">
            <img v-if="session.state.user?.avatar_url" :src="session.state.user.avatar_url" alt="">
            <i v-else class="ri-user-line" aria-hidden="true"></i>
            <span class="user-button-name">{{ session.state.user?.name || session.state.user?.login || 'admin' }}</span>
            <i class="ri-arrow-down-s-line" aria-hidden="true"></i>
          </el-button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item @click="goHome"><i class="ri-home-3-line"></i>{{ t('statusHome') }}</el-dropdown-item><el-dropdown-item @click="session.logout"><i class="ri-logout-box-r-line"></i>{{ t('logout') }}</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
      </header>
      <main id="main-content" class="admin-content"><slot /></main>
    </div>
    <button v-if="mobileOpen" type="button" class="sidebar-backdrop" :aria-label="t('closeNavigation')" @click="mobileOpen=false"></button>
    <AppDrawer :model-value="messages.panelOpen" :title="drawerTitle" mode="view" size="min(360px,92vw)" @update:model-value="onPanelOpen">
      <div class="message-panel">
        <template v-if="activeMessage">
          <el-button class="message-back" text @click="messages.closeDetail()"><i class="ri-arrow-left-line"></i>{{ t('back') }}</el-button>
          <dl class="message-detail">
            <div><dt>{{ t('occurredAt') }}</dt><dd>{{ formatDateTime(activeMessage.createdAt, locale) }}</dd></div>
            <div><dt>{{ t('message') }}</dt><dd>{{ activeMessage.message }}</dd></div>
            <div v-if="activeMessage.code"><dt>{{ t('errorCode') }}</dt><dd class="mono">{{ activeMessage.code }}</dd></div>
            <div v-if="activeMessage.status"><dt>{{ t('httpStatus') }}</dt><dd>{{ activeMessage.status }}</dd></div>
            <div v-if="activeMessage.traceId"><dt>{{ t('traceId') }}</dt><dd class="mono">{{ activeMessage.traceId }}</dd></div>
            <div v-if="activeMessage.route"><dt>{{ t('errorRoute') }}</dt><dd class="mono">{{ activeMessage.route }}</dd></div>
            <div v-if="activeMessage.detail"><dt>{{ t('details') }}</dt><dd>{{ activeMessage.detail }}</dd></div>
          </dl>
          <div v-if="fieldEntries.length" class="message-fields">
            <h3>{{ t('fieldErrors') }}</h3>
            <ul>
              <li v-for="[field, errors] in fieldEntries" :key="field"><strong class="mono">{{ field }}</strong><span>{{ errors.join('; ') }}</span></li>
            </ul>
          </div>
        </template>
        <template v-else>
          <div class="message-toolbar">
            <el-button text :disabled="!messages.unreadCount" @click="messages.markAllRead()"><i class="ri-check-double-line"></i>{{ t('markAllRead') }}</el-button>
            <el-button text :disabled="!messages.items.length" @click="clearAll"><i class="ri-delete-bin-line"></i>{{ t('clearMessages') }}</el-button>
          </div>
          <AppEmpty v-if="!messages.items.length" class="message-empty" icon="ri-notification-badge-line" :description="t('noMessages')" />
          <ul v-else class="message-list">
            <li v-for="item in messages.items" :key="item.id" class="message-list-item" :class="{ unread: !item.read }" @click="openItem(item)">
              <div class="message-list-main">
                <strong :class="{ 'message-unread': !item.read }">{{ item.message }}</strong>
                <div class="message-list-meta">
                  <small>{{ formatDateTime(item.createdAt, locale) }}</small>
                  <span v-if="item.code" class="mono message-code">{{ item.code }}</span>
                </div>
              </div>
              <el-button text circle :aria-label="t('delete')" @click.stop="messages.remove(item.id)"><i class="ri-delete-bin-line"></i></el-button>
            </li>
          </ul>
        </template>
      </div>
    </AppDrawer>
    <ScriptCommandsDrawer v-model="scriptsOpen" />
  </div>
</template>
