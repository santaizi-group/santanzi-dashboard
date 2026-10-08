import { describe, expect, it } from 'vitest'
import { missedObserverText } from './incidents'

describe('missed observers', () => {
  it('lists observers that did not see the host', () => {
    const text = missedObserverText([
      { observer_kind: 'primary', seen: true },
      { observer_name: '上海', seen: false },
      { observer_id: 'edge-b', seen: false },
    ], item => item.observer_name || item.observer_id || '')
    expect(text).toBe('上海, edge-b')
  })

  it('is empty when every observer saw the host', () => {
    expect(missedObserverText([{ observer_name: '主面板', seen: true }], item => item.observer_name || '')).toBe('')
  })
})
