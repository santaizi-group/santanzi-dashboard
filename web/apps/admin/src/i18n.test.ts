import { describe, expect, it } from 'vitest'
import { messages } from '@santaizi/i18n'

function strings(value: unknown): string[] {
  if (typeof value === 'string') return [value]
  if (!value || typeof value !== 'object') return []
  return Object.values(value).flatMap(strings)
}

describe('admin translations', () => {
  it('defines the same complete key set for every supported locale', () => {
    const expected = Object.keys(messages['zh-CN']).sort()
    for (const locale of ['zh-TW', 'en-US', 'es-ES'] as const) {
      expect(Object.keys(messages[locale]).sort()).toEqual(expected)
    }
  })

  it('resolves nested API error keys', () => {
    expect(messages['zh-CN'].errors.bot_test_failed).toBe('Token 测试失败')
  })

  it('does not leak Chinese fallback text into English or Spanish', () => {
    for (const locale of ['en-US', 'es-ES'] as const) {
      expect(strings(messages[locale]).filter(value => /[\u3400-\u9fff]/u.test(value))).toEqual([])
    }
  })

  it('keeps removed remote-operation labels out of the UI contract', () => {
    for (const locale of Object.keys(messages) as Array<keyof typeof messages>) {
      expect(messages[locale]).not.toHaveProperty('tasks')
      expect(messages[locale]).not.toHaveProperty('terminal')
      expect(messages[locale]).not.toHaveProperty('fileManager')
      expect(messages[locale]).not.toHaveProperty('forceUpdate')
    }
  })
})
