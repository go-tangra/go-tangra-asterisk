import { test, expect } from '@playwright/test'
export { test, expect }
export function requirePortal(){if(!process.env.E2E_BASE || !process.env.E2E_STORAGE_STATE)throw new Error('Set E2E_BASE and E2E_STORAGE_STATE for authenticated V4 portal acceptance')}
