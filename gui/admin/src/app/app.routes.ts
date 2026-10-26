import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: 'faqs',
    title: 'FAQs — crud-maker demo',
    loadComponent: () => import('./demo/faqs-demo').then((m) => m.FaqsDemo),
  },
  { path: '', pathMatch: 'full', redirectTo: 'faqs' },
];
