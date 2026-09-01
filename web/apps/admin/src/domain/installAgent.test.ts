import { describe, expect, it } from 'vitest'
import { DEFAULT_CLEAN_INSTALL, DEFAULT_INSTALL_PLATFORM, DEFAULT_INSTALL_PROFILE, INSTALL_PRESETS, defaultInstallPreviewBody } from './installAgent'

describe('defaultInstallPreviewBody', () => {
  it('matches the install dialog opening defaults', () => {
    expect(defaultInstallPreviewBody()).toEqual({
      platform: DEFAULT_INSTALL_PLATFORM,
      clean_install: DEFAULT_CLEAN_INSTALL,
      implementation: 'go',
      options: INSTALL_PRESETS[DEFAULT_INSTALL_PROFILE],
    })
    expect(DEFAULT_INSTALL_PLATFORM).toBe('linux')
    expect(DEFAULT_INSTALL_PROFILE).toBe('standard_cloud')
    expect(DEFAULT_CLEAN_INSTALL).toBe(true)
  })
})
