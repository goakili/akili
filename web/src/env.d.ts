// Copyright (c) 2026 Jonas Kaninda.
// SPDX-License-Identifier: Apache-2.0

/// <reference types="vite/client" />
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}
