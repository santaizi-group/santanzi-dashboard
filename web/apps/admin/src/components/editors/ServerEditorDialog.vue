<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { createServer, listDDNSProfiles, listNotificationGroups, listServerGroups, listTrafficPolicies, updateServer, type ServerRecord } from '@/api/adminApi'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import type { DDNSProfileRecord, TrafficPolicyRecord } from '@/types/admin'
import { hostAddresses } from '@/domain/hostAddress'
import PublicNoteEditor from './PublicNoteEditor.vue'
import TrafficPoliciesEditor from './TrafficPoliciesEditor.vue'

const props = defineProps<{ modelValue: boolean; value?: ServerRecord }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [ServerRecord, boolean] }>()
const { t, te } = useI18n()
const formRef = ref<FormInstance>()
const activeTab = ref('basic')
const saving = ref(false)
const loading = ref(false)
const ddnsProfiles = ref<DDNSProfileRecord[]>([])
const notificationGroups = ref<string[]>(['default'])
const serverGroups = ref<string[]>([])
const policies = ref<TrafficPolicyRecord[]>([])
const form = reactive({ id: 0, name: '', tag: '', note: '', public_note: '', monitoring_options: {} as Record<string, boolean>, display_index: 0, hide_for_guest: false, enable_ddns: false, ddns_profiles: [] as number[], probe_target: '', probe_tcp_ports: '', probe_enable_icmp: true, probe_enable_tcp: true, probe_enable_mtr: true })
const reported = reactive({ ipv4: '', ipv6: '' })
const snapshotValue = computed(() => ({ form, policies: policies.value }))
const { dirty, capture } = useEditorSnapshot(snapshotValue, computed(() => props.modelValue))
const groupOptions = computed(() => {
  const values = new Set(serverGroups.value)
  if (form.tag) values.add(form.tag)
  return [...values].sort((a, b) => a.localeCompare(b))
})

async function reset(value?: ServerRecord) {
  Object.assign(form, {
    id: value?.id || 0, name: value?.name || '', tag: value?.tag || '', note: value?.note || '',
    public_note: value?.public_note ? JSON.stringify(value.public_note, null, 2) : '', monitoring_options: { ...(value?.monitoring_options || {}) },
    display_index: value?.display_index || 0, hide_for_guest: value?.hide_for_guest ?? false,
    enable_ddns: value?.enable_ddns ?? false, ddns_profiles: [...(value?.ddns_profiles || [])],
    probe_target: value?.probe_target || '',
    probe_tcp_ports: value?.probe_tcp_ports || '',
    probe_enable_icmp: value?.probe_enable_icmp ?? true,
    probe_enable_tcp: value?.probe_enable_tcp ?? true,
    probe_enable_mtr: value?.probe_enable_mtr ?? true,
  })
  Object.assign(reported, hostAddresses(value?.host))
  activeTab.value = 'basic'; loading.value = true
  try {
    const [profiles, groups, traffic, serverGroupResult] = await Promise.all([
      listDDNSProfiles({ page: 1, page_size: 1000, sort: 'name', order: 'asc' }),
      listNotificationGroups(),
      value?.id ? listTrafficPolicies(value.id) : Promise.resolve({ data: [], meta: {} }),
      listServerGroups(),
    ])
    ddnsProfiles.value = profiles.data
    notificationGroups.value = groups.length ? groups : ['default']
    serverGroups.value = serverGroupResult.data.map(item => item.name).filter(Boolean)
    policies.value = traffic.data.map(item => ({ ...item }))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { loading.value = false; await nextTick(); capture() }
}
function hasIncompletePolicy() {
  return policies.value.some(item => !item.name.trim() || (item.mode === 'recurring' && !item.cycle_start))
}
async function submit() {
  await formRef.value?.validate()
  if (hasIncompletePolicy()) { activeTab.value = 'traffic'; ElMessage.error(t('trafficPolicyIncomplete')); return }
  saving.value = true
  try {
    const { id, public_note, ...rest } = form
    const traffic_policies = policies.value.map(({ usage: _usage, server_id: _serverID, ...policy }) => policy)
    const payload = { ...rest, public_note: public_note.trim() ? JSON.parse(public_note) as Record<string, unknown> : {}, traffic_policies }
    const created = !id
    const server = id ? await updateServer(id, payload) : await createServer(payload)
    capture(); emit('update:modelValue', false); emit('saved', server, created); ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}
watch(() => props.modelValue, value => { if (value) void reset(props.value) })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="form.id ? t('editServer') : t('createServer')" mode="edit" :dirty="dirty" :submitting="saving" width="min(1080px, 97vw)" @update:model-value="emit('update:modelValue', $event)">
    <div v-loading="loading">
      <el-form ref="formRef" :model="form" label-position="top" @submit.prevent="submit">
        <el-tabs v-model="activeTab" class="server-editor-tabs">
          <el-tab-pane :label="t('basicInformation')" name="basic">
            <div class="editor-grid">
              <el-form-item :label="t('serverName')" prop="name" :rules="[{ required: true, message: t('required') }]"><el-input v-model="form.name" /></el-form-item>
              <el-form-item :label="t('group')"><el-select v-model="form.tag" filterable allow-create default-first-option clearable class="field-full" :placeholder="t('PleaseSelect')"><el-option v-for="group in groupOptions" :key="group" :label="group" :value="group" /></el-select></el-form-item>
              <el-form-item :label="t('displayIndex')"><el-input v-model.number="form.display_index" inputmode="numeric" class="field-full" /></el-form-item>
              <el-form-item :label="t('hideForGuest')"><el-switch v-model="form.hide_for_guest" /></el-form-item>
              <template v-if="form.id && (reported.ipv4 || reported.ipv6)">
                <el-form-item :label="t('ipv4')"><el-input :model-value="reported.ipv4 || '—'" disabled /></el-form-item>
                <el-form-item :label="t('ipv6')"><el-input :model-value="reported.ipv6 || '—'" disabled /></el-form-item>
              </template>
              <el-form-item :label="t('probeTarget')"><el-input v-model="form.probe_target" /></el-form-item>
              <el-form-item :label="t('tcpPorts')"><el-input v-model="form.probe_tcp_ports" :disabled="!form.probe_enable_tcp" /></el-form-item>
              <el-form-item class="span-2">
                <div class="switch-grid switch-grid-3">
                  <label><span>{{ t('enableICMP') }}</span><el-switch v-model="form.probe_enable_icmp" /></label>
                  <label><span>{{ t('enableTCP') }}</span><el-switch v-model="form.probe_enable_tcp" /></label>
                  <label><span>{{ t('enableMTR') }}</span><el-switch v-model="form.probe_enable_mtr" /></label>
                </div>
              </el-form-item>
              <el-form-item class="span-2" :label="t('note')"><el-input v-model="form.note" type="textarea" :rows="10" maxlength="4000" show-word-limit /></el-form-item>
            </div>
          </el-tab-pane>
          <el-tab-pane :label="t('publicNote')" name="public-note"><PublicNoteEditor v-model="form.public_note" /></el-tab-pane>
          <el-tab-pane :label="t('trafficPolicies')" name="traffic"><TrafficPoliciesEditor v-model="policies" :notification-groups="notificationGroups" /></el-tab-pane>
          <el-tab-pane :label="t('ddnsAssociation')" name="ddns">
            <div class="switch-grid"><label><span>{{ t('enableDDNS') }}</span><el-switch v-model="form.enable_ddns" /></label></div>
            <el-form-item v-if="form.enable_ddns" :label="t('ddnsProfiles')"><el-select v-model="form.ddns_profiles" multiple filterable class="field-full"><el-option v-for="profile in ddnsProfiles" :key="profile.id" :label="profile.name" :value="profile.id" /></el-select></el-form-item>
          </el-tab-pane>
        </el-tabs>
      </el-form>
    </div>
    <template #footer="{ close }"><el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button><el-button type="primary" :loading="saving" @click="submit">{{ t('save') }}</el-button></template>
  </AppDialog>
</template>
