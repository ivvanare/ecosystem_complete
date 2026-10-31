import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { API_URL } from './api-url.token';

describe('API_URL', () => {
  it('defaults to the local dev API root — the single place that URL lives (S7)', () => {
    TestBed.configureTestingModule({});
    expect(TestBed.inject(API_URL)).toBe('http://localhost:9000/v1');
  });

  it('repoints every consumer through one provider override', () => {
    TestBed.configureTestingModule({
      providers: [{ provide: API_URL, useValue: 'https://api.example.test/v1' }],
    });
    expect(TestBed.inject(API_URL)).toBe('https://api.example.test/v1');
  });
});
