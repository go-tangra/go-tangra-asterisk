// Shared datetime formatting for the Asterisk module.
//
// This module is a Module Federation remote built separately from the host,
// so it cannot share the host's `@vben/utils` formatter state. Instead the
// host publishes the user's resolved time-display preference (timezone +
// 12h/24h) on a well-known `window` global, and these helpers read it. Uses
// the native `Intl` API so no extra dependency (dayjs, etc.) is required.
//
// IMPORTANT: use these ONLY for display. Never use them to build values sent
// to the backend — serialization must stay in UTC via `.toISOString()`.
//
// Historically the Asterisk timestamps came from a FreePBX deployment operated
// in Bulgaria and were force-rendered in Europe/Sofia. That default is kept as
// DISPLAY_TZ (still exported for backward compatibility) but the user's shared
// preference now takes precedence when set.

export const DISPLAY_TZ = 'Europe/Sofia';

const TIME_CONFIG_GLOBAL = '__TANGRA_TIME_CONFIG__';

interface TimeDisplayConfig {
  timeFormat: '12h' | '24h';
  timezone: string;
}

function readConfig(): TimeDisplayConfig {
  const raw = (globalThis as Record<string, any>)[TIME_CONFIG_GLOBAL] as
    | Partial<TimeDisplayConfig>
    | undefined;
  return {
    timeFormat: raw?.timeFormat === '12h' ? '12h' : '24h',
    timezone: typeof raw?.timezone === 'string' ? raw.timezone : '',
  };
}

export function formatDateTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const { timeFormat, timezone } = readConfig();
  try {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone || undefined,
      hour12: timeFormat === '12h',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(d);
    // en-CA yields "YYYY-MM-DD, HH:mm:ss"; drop the comma for a clean look.
    return parts.replace(',', '');
  } catch {
    return d.toLocaleString();
  }
}

export function formatDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const { timezone } = readConfig();
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone || undefined,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(d);
  } catch {
    return d.toLocaleDateString();
  }
}

export function formatTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const { timeFormat, timezone } = readConfig();
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone || undefined,
      hour12: timeFormat === '12h',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(d);
  } catch {
    return d.toLocaleTimeString();
  }
}
