import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, type UrlTree } from '@angular/router';
import { describe, expect, it, vi } from 'vitest';
import { ADMIN_ACCESS, adminAccessGuard } from './admin-access.guard';
import { AuthService } from './auth.service';

describe('adminAccessGuard', () => {
  const tree = { redirected: true } as unknown as UrlTree;

  function run(permissions: string[]): { result: unknown; createUrlTree: ReturnType<typeof vi.fn> } {
    TestBed.resetTestingModule();
    const createUrlTree = vi.fn().mockReturnValue(tree);
    TestBed.configureTestingModule({
      providers: [
        { provide: AuthService, useValue: { permissions: signal(permissions) } },
        { provide: Router, useValue: { createUrlTree } },
      ],
    });
    const result = TestBed.runInInjectionContext(() =>
      adminAccessGuard({} as never, {} as never),
    );
    return { result, createUrlTree };
  }

  it('lets a session carrying admin.access into the panel', () => {
    const { result, createUrlTree } = run([ADMIN_ACCESS, 'user.read']);

    expect(result).toBe(true);
    expect(createUrlTree).not.toHaveBeenCalled();
  });

  it('sends an authenticated session without admin.access to /no-access', () => {
    const { result, createUrlTree } = run(['user.read']);

    expect(result).toBe(tree);
    expect(createUrlTree).toHaveBeenCalledWith(['/no-access']);
  });
});
