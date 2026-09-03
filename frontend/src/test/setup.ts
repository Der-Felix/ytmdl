/**
 * Test lifecycle. Runs after src/test/dom.ts has registered the DOM.
 */

import { afterEach } from 'bun:test'
import { cleanup } from '@testing-library/react'

// A component left mounted would keep its timers and its event subscription,
// and the next test would see them.
afterEach(cleanup)
