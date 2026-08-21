<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getDatabase, getSettings, optimizeDatabase, updateSettings } from '@santaizi/api'
import type { DatabaseStatus } from '@santaizi/api'
import { notifyAPIError } from '@/composables/notify'
import { formatBytes } from '@/composables/format'
import LocationPicker from '@/components/LocationPicker.vue'
const { t, te, locale } = useI18n()
const saving = ref(false)
const optimizing = ref(false)
const dbStatus = ref<DatabaseStatus | null>(null)
const form = reactive<Record<string, unknown>>({
  site_title: '三太子监控', language: 'zh-CN', view_password: '', view_password_configured: false, clear_view_password: false,
  grpc_host: '', proxy_grpc_port: 0, tls: false, nameservers: [], show_availability_guest: true,
  primary_color: '#2563eb', footer_text: '', logo_url: '/static/logo.svg', custom_css: '', theme: 'server-status',
  allow_frontend_theme_switch: true, primary_location: '',
})
const dbLabel = computed(() => {
  if (!dbStatus.value) return ''
  return formatBytes(dbStatus.value.file_bytes, locale.value)
})
let pollTimer = 0
function stopPoll() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = 0
  }
}
async function loadDatabase() {
  try {
    const wasRunning = optimizing.value
    dbStatus.value = await getDatabase()
    optimizing.value = Boolean(dbStatus.value.running)
    if (wasRunning && !dbStatus.value.running) {
      stopPoll()
      if (dbStatus.value.last_run?.error) ElMessage.warning(t('requestFailed'))
      else ElMessage.success(t('optimizeDatabaseDone'))
    }
  } catch (e) {
    notifyAPIError(e, t as never, te)
  }
}
async function load() {
  try {
    Object.assign(form, await getSettings())
  } catch (e) {
    notifyAPIError(e, t as never, te)
  }
  await loadDatabase()
}
async function save() {
  saving.value = true
  try {
    Object.assign(form, await updateSettings(form))
    form.clear_view_password = false
    ElMessage.success(t('saveSuccess'))
  } catch (e) {
    notifyAPIError(e, t as never, te)
  } finally {
    saving.value = false
  }
}
async function optimize() {
  await ElMessageBox.confirm(t('optimizeDatabaseConfirm'), t('confirm'), { type: 'warning' })
  try {
    dbStatus.value = await optimizeDatabase()
    optimizing.value = true
    ElMessage.success(t('optimizeDatabaseStarted'))
    stopPoll()
    pollTimer = window.setInterval(() => { void loadDatabase() }, 2000)
  } catch (e) {
    notifyAPIError(e, t as never, te)
  }
}
onMounted(load)
onUnmounted(stopPoll)
</script>
<template>
  <div class="page-head">
    <div class="settings-title">
      <h1>{{ t('settings') }}</h1>
      <span v-if="dbLabel" class="settings-db-size">{{ dbLabel }}</span>
    </div>
    <div class="page-actions">
      <el-button :loading="optimizing" @click="optimize"><i class="ri-database-2-line"></i>{{ t('optimizeDatabase') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save"><i class="ri-save-line"></i>{{ t('save') }}</el-button>
    </div>
  </div>
  <el-form :model="form" label-position="top" class="settings-stack">
    <section class="surface settings-section">
      <div class="settings-heading"><i class="ri-window-line"></i><div><h2>{{ t('site') }}</h2></div></div>
      <div class="form-grid">
        <el-form-item :label="t('siteTitle')"><el-input v-model="form.site_title"/></el-form-item>
        <el-form-item :label="t('language')">
          <el-select v-model="form.language" style="width:100%">
            <el-option label="简体中文" value="zh-CN"/>
            <el-option label="繁體中文" value="zh-TW"/>
            <el-option label="English" value="en-US"/>
            <el-option label="Español" value="es-ES"/>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('accessPassword')">
          <el-input v-model="form.view_password" type="password" show-password :placeholder="form.view_password_configured ? t('passwordConfigured') : ''">
            <template v-if="form.view_password_configured" #append>
              <el-button @click="form.clear_view_password=true;form.view_password_configured=false"><i class="ri-delete-bin-line"></i>{{ t('clearPassword') }}</el-button>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('grpcHost')"><el-input v-model="form.grpc_host"/></el-form-item>
        <el-form-item :label="t('grpcProxyPort')"><el-input v-model.number="form.proxy_grpc_port" inputmode="numeric" :placeholder="t('grpcProxyPortPlaceholder')" style="width:100%"/></el-form-item>
        <el-form-item :label="t('primaryLocation')"><LocationPicker :model-value="String(form.primary_location || '')" @update:model-value="form.primary_location = $event"/></el-form-item>
        <el-form-item :label="t('grpcTLS')"><el-switch v-model="form.tls"/></el-form-item>
        <el-form-item :label="t('showAvailabilityGuest')"><el-switch v-model="form.show_availability_guest"/></el-form-item>
        <el-form-item class="span-2" :label="t('nameservers')"><el-select v-model="form.nameservers" multiple allow-create filterable style="width:100%"/></el-form-item>
      </div>
    </section>
    <section class="surface settings-section">
      <div class="settings-heading"><i class="ri-palette-line"></i><div><h2>{{ t('appearance') }}</h2></div></div>
      <div class="form-grid">
        <el-form-item :label="t('publicTheme')">
          <el-select v-model="form.theme" style="width:100%">
            <el-option :label="t('themeServerStatus')" value="server-status"/>
            <el-option :label="t('themeNazhua')" value="nazhua"/>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('logoURL')"><el-input v-model="form.logo_url"/></el-form-item>
        <el-form-item :label="t('allowFrontendThemeSwitch')"><el-switch v-model="form.allow_frontend_theme_switch"/></el-form-item>
        <el-form-item :label="t('primaryColor')"><el-color-picker v-model="form.primary_color"/></el-form-item>
        <el-form-item class="span-2" :label="t('footerText')"><el-input v-model="form.footer_text"/></el-form-item>
        <el-form-item class="span-2" :label="t('customCSS')"><el-input v-model="form.custom_css" type="textarea" :rows="8" class="mono"/><small>{{ t('safeCSSRules') }}</small></el-form-item>
      </div>
    </section>
  </el-form>
</template>
