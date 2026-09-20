<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { canonicalIata, locationOptions } from '@santaizi/geo'

const model = defineModel<string>({ default: '' })
const { locale, t } = useI18n()
const options = computed(() => locationOptions(locale.value))
const selected = computed({
  get() {
    return canonicalIata(model.value) || model.value
  },
  set(value: string) {
    const next = String(value || '').trim()
    model.value = /^[a-z]{2,3}$/i.test(next) ? next.toUpperCase() : next
  },
})
</script>

<template>
  <el-select
    v-model="selected"
    filterable
    allow-create
    clearable
    default-first-option
    :placeholder="t('location')"
    style="width: 100%"
  >
    <el-option v-for="item in options" :key="item.code" :label="item.label" :value="item.code" />
  </el-select>
</template>
