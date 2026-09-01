const KNOWN = new Set([
  'alpine', 'aosc', 'apple', 'archlinux', 'centos', 'coreos', 'debian', 'fedora',
  'freebsd', 'gentoo', 'linuxmint', 'mageia', 'mandriva', 'manjaro', 'nixos',
  'opensuse', 'redhat', 'raspberry-pi', 'sabayon', 'slackware', 'tux', 'ubuntu',
])

const ALIASES: Record<string, string> = {
  darwin: 'apple',
  macos: 'apple',
  osx: 'apple',
  arch: 'archlinux',
  'arch linux': 'archlinux',
  rhel: 'redhat',
  'red hat': 'redhat',
  'red hat enterprise linux': 'redhat',
  opensuse: 'opensuse',
  'open suse': 'opensuse',
  mint: 'linuxmint',
  'linux mint': 'linuxmint',
  linux: 'tux',
  gnu: 'tux',
  'gnu/linux': 'tux',
  raspberrypi: 'raspberry-pi',
  'raspberry pi': 'raspberry-pi',
}

const LABELS: Record<string, string> = {
  windows: 'Windows',
  linux: 'Linux',
  darwin: 'macOS',
  macos: 'macOS',
  debian: 'Debian',
  ubuntu: 'Ubuntu',
  centos: 'CentOS',
  fedora: 'Fedora',
  alpine: 'Alpine',
  arch: 'Arch',
  archlinux: 'Arch',
  freebsd: 'FreeBSD',
  opensuse: 'openSUSE',
  redhat: 'Red Hat',
  nixos: 'NixOS',
  manjaro: 'Manjaro',
  gentoo: 'Gentoo',
  aosc: 'AOSC',
  apple: 'macOS',
  coreos: 'CoreOS',
  linuxmint: 'Linux Mint',
  mageia: 'Mageia',
  mandriva: 'Mandriva',
  'raspberry-pi': 'Raspberry Pi',
  sabayon: 'Sabayon',
  slackware: 'Slackware',
  tux: 'Linux',
}

function lookupLabel(key: string): string | undefined {
  if (LABELS[key]) return LABELS[key]
  const mapped = ALIASES[key] || key.replace(/\s+/g, '')
  return LABELS[mapped]
}

function capitalizeFirst(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}

export function osLogoClass(platform?: string) {
  const raw = (platform || '').toLowerCase().trim()
  if (!raw) return 'ri-computer-line'
  if (raw.includes('windows') || raw.includes('microsoft')) return 'ri-microsoft-fill'
  const mapped = ALIASES[raw] || raw.replace(/\s+/g, '')
  return KNOWN.has(mapped) ? `fl-${mapped}` : 'ri-computer-line'
}

export function osLabel(platform?: string) {
  const raw = (platform || '').trim()
  if (!raw) return ''
  const key = raw.toLowerCase()
  if (key.includes('windows') || key.includes('microsoft')) return 'Windows'
  return lookupLabel(key) || capitalizeFirst(raw)
}
