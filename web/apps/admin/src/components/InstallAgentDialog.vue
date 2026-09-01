<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { getProbeCapabilities, getServerCredential, getServerInstallPreview, type ServerRecord } from '@/api/adminApi'
import type { InstallPreviewWrite } from '@santaizi/api'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import { DEFAULT_CLEAN_INSTALL, DEFAULT_INSTALL_IMPLEMENTATION, DEFAULT_INSTALL_PLATFORM, DEFAULT_INSTALL_PROFILE, INSTALL_PRESETS, type InstallProfile } from '@/domain/installAgent'
import type { MonitoringOptions, ProbeCapabilitiesMetadata } from '@/types/admin'

const props = defineProps<{ modelValue: boolean; server?: ServerRecord; secret?: string }>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()
const { t, te } = useI18n()
const loading = ref(false)
const platform = ref<'linux' | 'macos' | 'windows'>(DEFAULT_INSTALL_PLATFORM)
const implementation = ref<NonNullable<InstallPreviewWrite['implementation']>>(DEFAULT_INSTALL_IMPLEMENTATION)
const profile = ref<InstallProfile>(DEFAULT_INSTALL_PROFILE)
const cleanInstall = ref(DEFAULT_CLEAN_INSTALL)
const cleanConfirmed = ref(false)
const secret = ref('')
const command = ref('')
const metadata = ref<ProbeCapabilitiesMetadata>({ required: [], optional: [], presets: {} })
const capabilities = reactive<MonitoringOptions>({ ...INSTALL_PRESETS[DEFAULT_INSTALL_PROFILE] })
const ipReportConfig = reactive({ interface: '', country_code: '', prefer_ipv6: false })
const nicPresets = ['eth0', 'eth1', 'ens33', 'enp0s3', 'wlan0']
const snapshotValue = computed(() => ({
  implementation: implementation.value,
  profile: profile.value,
  cleanInstall: cleanInstall.value,
  capabilities: { ...capabilities },
  ipReportConfig: { ...ipReportConfig },
}))
const { dirty, capture } = useEditorSnapshot(snapshotValue, computed(() => props.modelValue))
const capabilityRows = computed(() => [
  ['cpu', 'ri-cpu-line'], ['memory', 'ri-database-2-line'], ['disk', 'ri-hard-drive-3-line'], ['network', 'ri-exchange-line'],
  ['connections', 'ri-links-line'], ['processes', 'ri-stack-line'], ['temperature', 'ri-temp-hot-line'], ['gpu', 'ri-cpu-line'],
  ['host_info', 'ri-computer-line'], ['ip_report', 'ri-map-pin-line'], ['http_probe', 'ri-global-line'], ['icmp_probe', 'ri-pulse-line'],
  ['tcp_probe', 'ri-router-line'], ['nat', 'ri-route-line'],
] as Array<[keyof MonitoringOptions, string]>)
const profileOptions = computed(() => [
  { label: t('presetStandardCloud'), value: 'standard_cloud' },
  { label: t('presetStandardPhysical'), value: 'standard_physical' },
  { label: t('presetLight'), value: 'light' },
  { label: t('presetHeartbeat'), value: 'alive' },
])
const implOptions = computed(() => [
  { label: t('agentImplGo'), value: 'go' },
  { label: t('agentImplRust'), value: 'rust' },
])
function applyProfile(value: InstallProfile) {
  profile.value = value
  const preset = metadata.value.presets[value] || INSTALL_PRESETS[value]
  Object.assign(capabilities, INSTALL_PRESETS[value], preset)
}
async function refreshPreview() {
  if (!props.server) return
  const preview = await getServerInstallPreview(props.server.id, {
    platform: platform.value,
    clean_install: cleanInstall.value,
    implementation: implementation.value,
    options: { ...capabilities },
    ip_report_config: capabilities.ip_report ? { ...ipReportConfig } : undefined,
  })
  command.value = preview.command
}
async function open() {
  if (!props.server) return
  loading.value = true
  platform.value = DEFAULT_INSTALL_PLATFORM
  implementation.value = DEFAULT_INSTALL_IMPLEMENTATION
  profile.value = DEFAULT_INSTALL_PROFILE
  cleanInstall.value = DEFAULT_CLEAN_INSTALL
  cleanConfirmed.value = false
  command.value = ''
  ipReportConfig.interface = ''
  ipReportConfig.country_code = ''
  ipReportConfig.prefer_ipv6 = false
  try {
    const [credential, available] = await Promise.all([
      props.secret ? Promise.resolve({ secret: props.secret }) : getServerCredential(props.server),
      getProbeCapabilities(),
    ])
    secret.value = credential.secret
    metadata.value = available
    applyProfile(DEFAULT_INSTALL_PROFILE)
    await refreshPreview()
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    loading.value = false
    await nextTick()
    capture()
  }
}
async function copy() {
  if (cleanInstall.value && !cleanConfirmed.value) {
    ElMessage.warning(t('confirmCleanInstallRequired'))
    return
  }
  await refreshPreview()
  await navigator.clipboard.writeText(command.value)
  capture()
  ElMessage.success(t('copied'))
}
async function copySecret() {
  await navigator.clipboard.writeText(secret.value)
  ElMessage.success(t('copied'))
}
function selectProfile(value: string | number | boolean) {
  applyProfile(value as InstallProfile)
}
function selectImplementation(value: string | number | boolean) {
  implementation.value = value === 'rust' ? 'rust' : 'go'
  if (implementation.value === 'rust') platform.value = DEFAULT_INSTALL_PLATFORM
}
function selectIPFamily(value: string | number | boolean) {
  ipReportConfig.prefer_ipv6 = value === 'ipv6'
}
watch(() => props.modelValue, value => { if (value) void open() })
watch([platform, snapshotValue], () => {
  command.value = ''
  if (props.modelValue && !loading.value) void refreshPreview()
}, { deep: true })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="`${t('installAgent')} · ${server?.name || ''}`" mode="edit" :dirty="dirty" :submitting="loading" width="min(960px, 97vw)" @update:model-value="emit('update:modelValue', $event)">
    <div v-loading="loading">
      <el-form label-position="top">
        <el-form-item :label="t('secret')">
          <el-input :model-value="secret" readonly class="mono">
            <template #append>
              <el-button :aria-label="t('copy')" @click="copySecret"><i class="ri-file-copy-line"></i></el-button>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-segmented :model-value="implementation" :options="implOptions" @change="selectImplementation" />
        </el-form-item>
        <template v-if="implementation === 'go'">
        <el-form-item :label="t('monitoringPreset')">
          <el-segmented :model-value="profile" :options="profileOptions" @change="selectProfile" />
        </el-form-item>
        <div class="capability-grid">
          <label v-for="([key, icon]) in capabilityRows" :key="key" class="capability-item">
            <span><i :class="icon"></i>{{ t(`capability_${key}`) }}</span>
            <el-switch v-model="capabilities[key]" />
          </label>
        </div>
        <div v-if="capabilities.ip_report" class="ip-report-box">
          <el-form-item :label="t('ipReportInterface')">
            <el-select v-model="ipReportConfig.interface" filterable allow-create default-first-option clearable :placeholder="t('ipReportInterfacePlaceholder')">
              <el-option v-for="name in nicPresets" :key="name" :label="name" :value="name" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('ipReportCountryCode')">
            <el-input v-model="ipReportConfig.country_code" placeholder="CN" maxlength="16" />
          </el-form-item>
          <el-form-item :label="t('ipReportFamily')">
            <el-segmented :model-value="ipReportConfig.prefer_ipv6 ? 'ipv6' : 'ipv4'" :options="[{ label: 'IPv4', value: 'ipv4' }, { label: 'IPv6', value: 'ipv6' }]" @change="selectIPFamily" />
          </el-form-item>
        </div>
        </template>
        <div class="clean-install-box">
          <el-checkbox v-model="cleanInstall">{{ t('cleanInstall') }}</el-checkbox>
          <el-checkbox v-if="cleanInstall" v-model="cleanConfirmed">{{ t('confirmCleanInstall') }}</el-checkbox>
          <p v-if="cleanInstall" class="clean-install-note">{{ t('cleanInstallLegacyNote') }}</p>
        </div>
      </el-form>
      <el-tabs v-if="implementation === 'go'" v-model="platform" class="install-tabs">
        <el-tab-pane :label="t('linux')" name="linux" />
        <el-tab-pane :label="t('macos')" name="macos" />
        <el-tab-pane :label="t('windows')" name="windows" />
      </el-tabs>
      <el-input :model-value="command" readonly type="textarea" :rows="6" class="mono" />
    </div>
    <template #footer="{ close }">
      <el-button :disabled="loading" @click="close()">{{ t('close') }}</el-button>
      <el-button type="primary" :disabled="loading || (cleanInstall && !cleanConfirmed)" @click="copy"><i class="ri-file-copy-line"></i>{{ t('copyCommand') }}</el-button>
    </template>
  </AppDialog>
</template>
