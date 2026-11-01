/**
 * Date rendering for the panel. One formatter instance per locale, reused: the
 * API always sends RFC 3339 in UTC (`api/API.md` §1), so the only decision left
 * is the reader's time zone, which the browser already knows.
 */
const DATE_TIME = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

/** `null`/malformed input renders as an em dash: never "Invalid Date" in a table. */
export function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : DATE_TIME.format(date);
}
