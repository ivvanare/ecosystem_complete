import { Routes } from '@angular/router';
import { authGuard } from './core/auth.guard';

export const routes: Routes = [
  {
    path: 'login',
    title: 'Sign in — melZone admin',
    loadComponent: () => import('./auth/login').then((m) => m.Login),
  },
  {
    path: 'faqs',
    title: 'FAQs — crud-maker demo',
    canActivate: [authGuard],
    loadComponent: () => import('./demo/faqs-demo').then((m) => m.FaqsDemo),
  },
  { path: '', pathMatch: 'full', redirectTo: 'faqs' },
];
