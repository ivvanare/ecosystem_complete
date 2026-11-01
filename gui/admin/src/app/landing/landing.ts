import { Component } from '@angular/core';
import { RouterLink } from '@angular/router';

/**
 * Public landing at `/` (S2): the product page for melZone, with the only
 * call to action that goes anywhere today — the panel sign-in. It renders for
 * an anonymous visitor and issues no API request, so a dead API never blocks
 * the front door.
 *
 * The app preview inside the hero is illustrative content, not live data;
 * nothing on this page claims a number the API cannot prove.
 */
@Component({
  selector: 'app-landing',
  imports: [RouterLink],
  templateUrl: './landing.html',
  styleUrl: './landing.css',
})
export class Landing {}
