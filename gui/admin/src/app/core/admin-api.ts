import { HttpClient, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { API_URL } from './api-url.token';

/** Envelope every list endpoint answers with (`api/API.md` §1). */
export interface Page<T> {
  data: T[];
  page: number;
  limit: number;
  total: number;
}

export type UserStatus = 'ACTIVE' | 'INACTIVE' | 'SUSPENDED';

/** `UserSummary` in `api/openapi.yaml`. */
export interface UserSummary {
  id: string;
  name: string | null;
  email: string | null;
  status: UserStatus;
  email_verified_at: string | null;
  roles: string[];
  created_at: string;
}

/** `Role` in `api/openapi.yaml`. */
export interface Role {
  id: string;
  name: string;
  description: string | null;
  status: 'ACTIVE' | 'INACTIVE';
  permissions: string[];
}

/** `Permission` in `api/openapi.yaml`. */
export interface Permission {
  id: string;
  code: string;
  module: string;
  name: string;
  status: 'ACTIVE' | 'INACTIVE';
}

/** `AuditEvent` in `api/openapi.yaml`. Emails are never in clear: only an HMAC. */
export interface AuditEvent {
  id: string;
  event: string;
  user_id: string | null;
  actor_user_id: string | null;
  email_hash: string | null;
  ip: string | null;
  country: string | null;
  city: string | null;
  user_agent: string | null;
  metadata: Record<string, unknown> | null;
  created_at: string;
}

export interface PageQuery {
  page: number;
  limit: number;
}

export interface UsersQuery extends PageQuery {
  /** Case-insensitive match on name or email. */
  q?: string | null;
  status?: UserStatus | null;
}

export interface AuditQuery extends PageQuery {
  /** Exact event name, e.g. `auth.login.failed`. */
  event?: string | null;
  userId?: string | null;
  from?: string | null;
  to?: string | null;
}

/**
 * The admin endpoints the panel uses, with this API's dialect. It exists because
 * `crud-maker`'s `HttpTransport` speaks a different one (`perPage` instead of
 * `limit`, and a `field_kind` filter grammar this API does not have), and because
 * these entities are read-only here: role assignment is a replacement call, not
 * a generic update.
 *
 * Errors propagate as `HttpErrorResponse`; callers parse `error.error.code`,
 * never `message` (`api/API.md` §1).
 */
@Injectable({ providedIn: 'root' })
export class AdminApi {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = inject(API_URL).replace(/\/+$/, '');

  listUsers(query: UsersQuery): Promise<Page<UserSummary>> {
    return firstValueFrom(
      this.http.get<Page<UserSummary>>(`${this.baseUrl}/admin/users`, {
        params: toParams({
          page: query.page,
          limit: query.limit,
          q: query.q,
          status: query.status,
        }),
      }),
    );
  }

  /** Replaces the whole role set: a role left out of `roles` is removed. */
  setUserRoles(userId: string, roles: string[]): Promise<UserSummary> {
    return firstValueFrom(
      this.http.patch<UserSummary>(
        `${this.baseUrl}/admin/users/${encodeURIComponent(userId)}/roles`,
        { roles },
      ),
    );
  }

  async listRoles(): Promise<Role[]> {
    const body = await firstValueFrom(
      this.http.get<{ data: Role[] }>(`${this.baseUrl}/admin/roles`),
    );
    return body.data;
  }

  async listPermissions(): Promise<Permission[]> {
    const body = await firstValueFrom(
      this.http.get<{ data: Permission[] }>(`${this.baseUrl}/admin/permissions`),
    );
    return body.data;
  }

  listAuditEvents(query: AuditQuery): Promise<Page<AuditEvent>> {
    return firstValueFrom(
      this.http.get<Page<AuditEvent>>(`${this.baseUrl}/admin/audit-events`, {
        params: toParams({
          page: query.page,
          limit: query.limit,
          event: query.event,
          userId: query.userId,
          from: query.from,
          to: query.to,
        }),
      }),
    );
  }
}

/** Drops empty values so an absent filter never travels as `?q=`. */
function toParams(values: Record<string, string | number | null | undefined>): HttpParams {
  let params = new HttpParams();
  for (const [key, value] of Object.entries(values)) {
    if (value === null || value === undefined) continue;
    const text = String(value).trim();
    if (text === '') continue;
    params = params.set(key, text);
  }
  return params;
}
