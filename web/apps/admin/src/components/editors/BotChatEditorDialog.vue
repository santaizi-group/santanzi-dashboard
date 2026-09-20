<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { listNotificationGroups, updateBotChat } from '@/api/adminApi'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import type { BotChatRecord, BotRole } from '@/types/admin'

const props = defineProps<{ modelValue: boolean; value?: BotChatRecord }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [] }>()
const { t, te } = useI18n()
const formRef = ref<FormInstance>()
const saving = ref(false)
const groups = ref<string[]>([])
const form = reactive<{ id: number; role: BotRole; enabled: boolean; allowed_user_ids: string[]; subscribe_tags: string[]; note: string }>({
  id: 0, role: 'viewer', enabled: true, allowed_user_ids: [], subscribe_tags: [], note: '',
})
const { dirty, capture } = useEditorSnapshot(form, computed(() => props.modelValue))
const isGroup = computed(() => {
  const kind = props.value?.kind || ''
  return kind === 'group' || kind === 'supergroup'
})

function reset(value?: BotChatRecord) {
  Object.assign(form, {
    id: value?.id || 0,
    role: value?.role || 'viewer',
    enabled: value?.enabled ?? true,
    allowed_user_ids: (value?.allowed_user_ids || []).map(id => String(id)),
    subscribe_tags: [...(value?.subscribe_tags || [])],
    note: value?.note || '',
  })
  nextTick(capture)
}

async function loadGroups() {
  try { groups.value = await listNotificationGroups() }
  catch (error) { notifyAPIError(error, t as never, te) }
}

async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    await updateBotChat(form.id, {
      role: form.role,
      enabled: form.enabled,
      allowed_user_ids: form.allowed_user_ids.map(id => Number(id)).filter(id => Number.isFinite(id) && id !== 0),
      subscribe_tags: form.subscribe_tags,
      note: form.note,
    })
    capture(); emit('update:modelValue', false); emit('saved'); ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}

watch(() => props.modelValue, value => { if (value) { reset(props.value); void loadGroups() } })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="t('editBotChat')" mode="edit" :dirty="dirty" :submitting="saving" width="min(720px, 96vw)" @update:model-value="emit('update:modelValue', $event)">
    <el-alert v-if="isGroup" type="warning" :closable="false" show-icon :title="t('botGroupRoleWarning')" style="margin-bottom: 16px" />
    <el-form ref="formRef" :model="form" label-position="top" @submit.prevent="submit">
      <div class="editor-grid">
        <el-form-item :label="t('botRole')" prop="role" :rules="[{ required: true, message: t('required') }]">
          <el-select v-model="form.role" class="field-full">
            <el-option :label="t('botRoleViewer')" value="viewer" />
            <el-option :label="t('botRoleOperator')" value="operator" />
            <el-option :label="t('botRoleAdmin')" value="admin" />
            <el-option :label="t('botRoleBlocked')" value="blocked" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('enabled')"><el-switch v-model="form.enabled" /></el-form-item>
        <el-form-item class="span-2" :label="t('botAllowedUsers')">
          <el-select v-model="form.allowed_user_ids" multiple filterable allow-create default-first-option class="field-full" />
        </el-form-item>
        <el-form-item class="span-2" :label="t('botSubscribeTags')">
          <el-select v-model="form.subscribe_tags" multiple filterable allow-create default-first-option class="field-full">
            <el-option :label="t('botSubscribeAll')" value="*" />
            <el-option v-for="group in groups" :key="group" :label="group" :value="group" />
          </el-select>
        </el-form-item>
        <el-form-item class="span-2" :label="t('note')"><el-input v-model="form.note" type="textarea" :rows="3" /></el-form-item>
      </div>
    </el-form>
    <template #footer="{ close }"><el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button><el-button type="primary" :loading="saving" @click="submit">{{ t('save') }}</el-button></template>
  </AppDialog>
</template>
