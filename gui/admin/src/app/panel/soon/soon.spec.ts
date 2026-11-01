import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, provideRouter } from '@angular/router';
import { describe, expect, it } from 'vitest';
import { Soon } from './soon';

describe('Soon', () => {
  function render(data: Record<string, unknown>): HTMLElement {
    TestBed.configureTestingModule({
      imports: [Soon],
      providers: [
        provideRouter([]),
        { provide: ActivatedRoute, useValue: { snapshot: { data } } },
      ],
    });
    const fixture = TestBed.createComponent(Soon);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('renders the module the route declares, and what it will do', () => {
    const element = render({
      title: 'Places',
      description: 'Venue records with schedules, photos and their social links.',
      permission: 'place.create',
      bullets: ['Create and edit a venue', 'Per-day schedules'],
    });
    const text = element.textContent ?? '';

    expect(text).toContain('Coming soon');
    expect(text).toContain('Places');
    expect(text).toContain('Per-day schedules');
    expect(text).toContain('place.create');
    expect(element.querySelector('a[href="/panel/overview"]')).toBeTruthy();
  });

  it('works without a permission or bullets — the data is optional by design', () => {
    const element = render({ title: 'Routes', description: 'Ordered visit lists.' });
    const text = element.textContent ?? '';

    expect(text).toContain('Routes');
    expect(text).not.toContain('undefined');
  });
});
