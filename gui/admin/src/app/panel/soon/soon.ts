import { Component, inject } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';

/** Shape the router's `data` carries for every "soon" module (see `app.routes.ts`). */
export interface SoonScreenData {
  title: string;
  description: string;
  /** Permission code that will govern the module once the API exists. */
  permission?: string;
  bullets?: string[];
}

/**
 * One screen for the four modules that have a place in the navigation but no API
 * yet (S4): places, menus, reviews and routes. It states what the module will do
 * and issues no request, so it can never look like a broken module that failed
 * to load data.
 */
@Component({
  selector: 'app-soon',
  imports: [RouterLink],
  templateUrl: './soon.html',
  styleUrl: './soon.css',
})
export class Soon {
  protected readonly data = inject(ActivatedRoute).snapshot.data as SoonScreenData;
  protected readonly bullets = this.data.bullets ?? [];
}
