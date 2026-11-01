import { HttpErrorResponse } from '@angular/common/http';

/**
 * Extracts the machine-readable code from the API error envelope
 * (`{"error":{"code","message"}}`). Clients parse `code`, never `message`
 * (`api/API.md` §1). A failure with no envelope — network down, CORS, a proxy
 * page — reads as `network_error` so the UI always has something to show.
 */
export function apiErrorCode(error: unknown): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { error?: { code?: unknown } } | null;
    const code = body?.error?.code;
    if (typeof code === 'string' && code !== '') {
      return code;
    }
  }
  return 'network_error';
}
