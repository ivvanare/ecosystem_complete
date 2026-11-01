import { Component } from '@angular/core';

/**
 * Same idea as the app preview, for the panel: a static picture of the sidebar,
 * the indicators and the activity chart. Purely presentational; it never reads
 * the API, because a landing page that needs the API is a landing page that can
 * fail to load.
 */
@Component({
  selector: 'app-panel-preview',
  templateUrl: './panel-preview.html',
  styleUrl: './panel-preview.css',
})
export class PanelPreview {}
