/**
 * Demo entity config for the FAQ vertical — a plain object with exactly the
 * shape of a JSON document. This is what "add a CRUD in minutes" means:
 * declare fields, filters, sorts, formatters and actions here, and the
 * engine + this screen do the rest.
 */
export const FAQS_CONFIG = {
  key: 'faqs',
  endpoint: 'faqs',
  primaryKey: 'id',
  titleField: 'question',
  pageSize: 5,
  searchFields: ['question'],
  fields: [
    { name: 'id', label: 'ID', sortable: false, in: { form: false, detail: false } },
    {
      name: 'question',
      label: 'Question',
      required: true,
      filters: ['like'],
      formatter: { name: 'truncate', params: { length: 48 } },
      validators: [{ name: 'minLength', params: 5 }],
    },
    {
      name: 'answer',
      label: 'Answer',
      type: 'textarea',
      required: true,
      in: { form: true, detail: true },
    },
    {
      name: 'category',
      label: 'Category',
      type: 'select',
      filters: ['in'],
      options: [
        { label: 'General', value: 'general' },
        { label: 'Reactivity', value: 'reactivity' },
        { label: 'Forms', value: 'forms' },
        { label: 'HTTP', value: 'http' },
      ],
      required: true,
      cell: {
        render: 'tag',
        variantMap: {
          general: 'slate',
          reactivity: 'indigo',
          forms: 'emerald',
          http: 'amber',
        },
      },
    },
    {
      name: 'views',
      label: 'Views',
      type: 'number',
      formatter: 'number',
      filters: ['between', 'gte'],
      validators: [{ name: 'min', params: 0 }],
      required: true,
    },
    {
      name: 'published',
      label: 'Published',
      type: 'boolean',
      formatter: 'boolean',
      filters: ['eq'],
      cell: {
        render: 'badge',
        variantMap: { true: 'success', false: 'muted' },
      },
    },
    {
      name: 'createdAt',
      label: 'Created',
      type: 'datetime',
      formatter: 'date',
      defaultSort: 'desc',
      in: { form: false, detail: true },
    },
  ],
} as const;

/** Demo rows for the in-memory transport. */
export const FAQS_SEED = [
  {
    id: 1,
    question: 'How do signals work?',
    answer: 'Signals are writable state values with fine-grained reactivity.',
    category: 'reactivity',
    views: 320,
    published: true,
    createdAt: '2026-09-14T09:30:00',
  },
  {
    id: 2,
    question: 'How do Signal Forms work?',
    answer: 'form(model) creates a FieldTree with type-safe bindings and schema validation.',
    category: 'forms',
    views: 180,
    published: true,
    createdAt: '2026-09-20T11:15:00',
  },
  {
    id: 3,
    question: 'What about httpResource?',
    answer: 'httpResource tracks reactive dependencies and fetches declaratively.',
    category: 'http',
    views: 96,
    published: false,
    createdAt: '2026-10-01T16:45:00',
  },
  {
    id: 4,
    question: 'Is zoneless the default now?',
    answer: 'In Angular v22 change detection is zoneless by default; OnPush semantics apply.',
    category: 'reactivity',
    views: 420,
    published: true,
    createdAt: '2026-10-05T08:00:00',
  },
  {
    id: 5,
    question: 'Do I still need NgModules?',
    answer: 'No. Standalone is the default since v20 and NgModules are legacy.',
    category: 'general',
    views: 75,
    published: false,
    createdAt: '2026-10-11T13:20:00',
  },
  {
    id: 6,
    question: 'How do I render lists efficiently?',
    answer: 'Use @for with a stable track expression and computed derived state.',
    category: 'general',
    views: 210,
    published: true,
    createdAt: '2026-10-18T10:05:00',
  },
];
