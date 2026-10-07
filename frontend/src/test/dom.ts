/**
 * Registers the DOM, and nothing else.
 *
 * This is a file of its own because import order decides whether it works:
 * @testing-library/dom binds its queries to document.body when it is
 * evaluated, and ES imports are hoisted, so a single setup file that imported
 * the library would have evaluated it before the registration ran.
 */

import { GlobalRegistrator } from '@happy-dom/global-registrator'

// Relative artwork URLs need a valid origin. Keep file loading disabled so
// component tests cannot contact a real server; errors are dispatched explicitly.
GlobalRegistrator.register({ url: 'http://localhost:59590', settings: { enableImageFileLoading: false } })

// React 19 reads this to decide whether it may use the DOM-only paths.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
