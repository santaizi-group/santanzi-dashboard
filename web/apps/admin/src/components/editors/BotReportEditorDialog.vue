<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { createBotReport, listAllServers, listBotChats, updateBotReport, type ServerRecord } from '@/api/adminApi'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import type { BotChatRecord, BotCover, BotPeriod, BotReportRecord, BotSection } from '@/types/admin'

const props = defineProps<{ modelValue: boolean; value?: BotReportRecord }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [] }>()
const { t, te } = useI18n()
const formRef = ref<FormInstance>()
const saving = ref(false)
const servers = ref<ServerRecord[]>([])
const chats = ref<BotChatRecord[]>([])
const form = reactive<{
  id: number; name: string; chat_ids: number[]; period: BotPeriod; hour_local: number; minute: number;
  weekday: number; day_of_month: number; sections: BotSection[]; cover: BotCover; ignore_ids: number[];
  with_charts: boolean; enabled: boolean
}>({
  id: 0, name: '', chat_ids: [], period: 'daily', hour_local: 9, minute: 0, weekday: 1, day_of_month: 1,
  sections: ['status', 'servers', 'traffic', 'uptime', 'probes', 'alerts'], cover: 'all', ignore_ids: [],
  with_charts: false, enabled: true,
})
const { dirty, capture } = useEditorSnapshot(form, computed(() => props.modelValue))
const transferData = computed(() => servers.value.map(server => ({ key: server.id, label: server.name })))
const sectionOptions = computed(() => ([
  { label: t('botSectionStatus'), value: 'status' },
  { label: t('botSectionServers'), value: 'servers' },
  { label: t('botSectionTraffic'), value: 'traffic' },
  { label: t('botSectionUptime'), value: 'uptime' },
  { label: t('botSectionProbes'), value: 'probes' },
  { label: t('botSectionAlerts'), value: 'alerts' },
]))
const weekdayOptions = computed(() => ([
  { label: t('weekdaySun'), value: 0 }, { label: t('weekdayMon'), value: 1 }, { label: t('weekdayTue'), value: 2 },
  { label: t('weekdayWed'), value: 3 }, { label: t('weekdayThu'), value: 4 }, { label: t('weekdayFri'), value: 5 },
  { label: t('weekdaySat'), value: 6 },
]))

function clampNumber(value: unknown, min: number, max: number, fallback: number) {
  const next = Number(value)
  if (!Number.isFinite(next)) return fallback
  return Math.min(max, Math.max(min, next))
}

function reset(value?: BotReportRecord) {
  Object.assign(form, {
    id: value?.id || 0,
    name: value?.name || '',
    chat_ids: [...(value?.chat_ids || [])],
    period: value?.period || 'daily',
    hour_local: value?.hour_local ?? 9,
    minute: value?.minute ?? 0,
    weekday: value?.weekday ?? 1,
    day_of_month: value?.day_of_month || 1,
    sections: value?.sections?.length ? [...value.sections] : ['status', 'servers', 'traffic', 'uptime', 'probes', 'alerts'],
    cover: value?.cover || 'all',
    ignore_ids: [...(value?.ignore_ids || [])],
    with_charts: value?.with_charts ?? false,
    enabled: value?.enabled ?? true,
  })
  nextTick(capture)
}

async function loadOptions() {
  try {
    const [serverResult, chatResult] = await Promise.all([
      listAllServers(),
      listBotChats({ page: 1, page_size: 200, sort: 'id', order: 'desc' }),
    ])
    servers.value = serverResult.data
    chats.value = chatResult.data
  } catch (error) { notifyAPIError(error, t as never, te) }
}

async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = {
      name: form.name,
      chat_ids: form.chat_ids,
      period: form.period,
      hour_local: form.hour_local,
      minute: form.minute,
      weekday: form.weekday,
      day_of_month: form.day_of_month,
      sections: form.sections,
      cover: form.cover,
      ignore_ids: form.cover === 'all' ? [] : form.ignore_ids,
      with_charts: form.with_charts,
      enabled: form.enabled,
    }
    if (form.id) await updateBotReport(form.id, payload)
    else await createBotReport(payload)
    capture(); emit('update:modelValue', false); emit('saved'); ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}

watch(() => props.modelValue, value => { if (value) { reset(props.value); void loadOptions() } })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="form.id ? t('editBotReport') : t('createBotReport')" mode="edit" :dirty="dirty" :submitting="saving" width="min(900px, 96vw)" @update:model-value="emit('update:modelValue', $event)">
    <el-form ref="formRef" :model="form" label-position="top" @submit.prevent="submit">
      <div class="editor-grid">
        <el-form-item :label="t('name')" prop="name" :rules="[{ required: true, message: t('required') }]"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('botPeriod')">
          <el-select v-model="form.period" class="field-full">
            <el-option :label="t('botPeriodDaily')" value="daily" />
            <el-option :label="t('botPeriodWeekly')" value="weekly" />
            <el-option :label="t('botPeriodMonthly')" value="monthly" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('botHour')"><el-input v-model.number="form.hour_local" inputmode="numeric" class="field-full" @blur="form.hour_local = clampNumber(form.hour_local, 0, 23, 9)" /></el-form-item>
        <el-form-item :label="t('botMinute')"><el-input v-model.number="form.minute" inputmode="numeric" class="field-full" @blur="form.minute = clampNumber(form.minute, 0, 59, 0)" /></el-form-item>
        <el-form-item v-if="form.period === 'weekly'" :label="t('botWeekday')">
          <el-select v-model="form.weekday" class="field-full"><el-option v-for="item in weekdayOptions" :key="item.value" :label="item.label" :value="item.value" /></el-select>
        </el-form-item>
        <el-form-item v-if="form.period === 'monthly'" :label="t('botDayOfMonth')">
          <el-input v-model.number="form.day_of_month" inputmode="numeric" class="field-full" @blur="form.day_of_month = clampNumber(form.day_of_month, 1, 31, 1)" />
        </el-form-item>
        <el-form-item :label="t('enabled')"><el-switch v-model="form.enabled" /></el-form-item>
        <el-form-item :label="t('botWithCharts')"><el-switch v-model="form.with_charts" /></el-form-item>
        <el-form-item class="span-2" :label="t('botReportChats')" prop="chat_ids" :rules="[{ type: 'array', required: true, min: 1, message: t('required') }]">
          <el-select v-model="form.chat_ids" multiple filterable class="field-full">
            <el-option v-for="chat in chats" :key="chat.id" :label="`${chat.title || chat.chat_id} (${chat.chat_id})`" :value="chat.chat_id" />
          </el-select>
        </el-form-item>
        <el-form-item class="span-2" :label="t('botSections')">
          <el-checkbox-group v-model="form.sections">
            <el-checkbox v-for="item in sectionOptions" :key="item.value" :value="item.value">{{ item.label }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item class="span-2" :label="t('serverScope')">
          <el-radio-group v-model="form.cover" class="scope-mode">
            <el-radio-button value="all">{{ t('scopeAll') }}</el-radio-button>
            <el-radio-button value="include">{{ t('scopeSelectedServers') }}</el-radio-button>
            <el-radio-button value="exclude">{{ t('scopeExceptSelected') }}</el-radio-button>
          </el-radio-group>
          <el-transfer
            v-if="form.cover !== 'all'"
            v-model="form.ignore_ids"
            filterable
            :filter-placeholder="t('searchServers')"
            :data="transferData"
            :titles="[t('availableServers'), form.cover === 'include' ? t('selectedServers') : t('excludedServers')]"
            class="server-transfer field-full"
          />
        </el-form-item>
      </div>
    </el-form>
    <template #footer="{ close }"><el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button><el-button type="primary" :loading="saving" @click="submit">{{ t('save') }}</el-button></template>
  </AppDialog>
</template>
