import { Component, ViewEncapsulation } from '@angular/core';

/**
 * Carries ng-zorro's stylesheet inside the panel's lazy chunk instead of the global
 * one. Measured: importing it in `styles.css` took the initial CSS from 12 kB to
 * 570 kB on *every* route, including the landing, which renders no zorro component.
 *
 * `ViewEncapsulation.None` is deliberate and load-bearing: Angular rewrites
 * component styles with `_ngcontent` attributes under emulated encapsulation, which
 * would stop `.ant-*` rules from matching zh-zorro's own elements. With none, the
 * rules stay global but are still emitted in the chunk that owns this component —
 * the panel — so the public pages never download them.
 */
@Component({
  selector: 'app-zorro-styles',
  template: '',
  styleUrl: './zorro-styles.css',
  encapsulation: ViewEncapsulation.None,
})
export class ZorroStyles {}
