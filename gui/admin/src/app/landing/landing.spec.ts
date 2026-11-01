import { provideRouter } from '@angular/router';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { Landing } from './landing';

describe('Landing', () => {
  function render(): HTMLElement {
    TestBed.configureTestingModule({ imports: [Landing], providers: [provideRouter([])] });
    const fixture = TestBed.createComponent(Landing);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('renders the public hero for an anonymous visitor', () => {
    const element = render();

    expect(element.querySelector('h1')?.textContent).toContain('Places, menus and routes');
    expect(element.textContent).toContain('All on one map');
  });

  it('sends the only real call to action to the panel sign-in', () => {
    const element = render();
    const links = [...element.querySelectorAll('a')].map((a) => a.getAttribute('href'));

    expect(links).toContain('/login');
    // Nothing on the landing pretends to be a panel route.
    expect(links.some((href) => href === '/panel')).toBe(false);
  });
});
