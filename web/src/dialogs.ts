import Swal from 'sweetalert2'

// The interface never opens the browser's own confirm or prompt. Those cannot
// carry the chosen language, cannot follow the palette, and block the page
// while they are open, which stops a streaming answer from painting.
//
// The buttons are the interface's own, so buttonsStyling is off and the classes
// below are the ones the rest of the interface already uses. The theme follows
// prefers-color-scheme, which is the signal the palette follows too.
//
// heightAuto is off because the default forces html and body to `height: auto`,
// which collapses the layout's full-height chain and leaves the interface as a
// short block at the top of the page for as long as a dialog is open.
const shared = {
  theme: 'auto',
  buttonsStyling: false,
  reverseButtons: true,
  showCancelButton: true,
  heightAuto: false,
} as const

export interface ConfirmOptions {
  title: string
  text: string
  confirmText: string
  cancelText: string
  /** Paints the confirming button as a destructive action. */
  danger?: boolean
}

/** Asks the user to confirm an action. Returns false when they refuse. */
export async function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  const result = await Swal.fire({
    ...shared,
    icon: 'warning',
    title: options.title,
    text: options.text,
    confirmButtonText: options.confirmText,
    cancelButtonText: options.cancelText,
    customClass: {
      confirmButton: options.danger ? 'danger' : 'primary',
      cancelButton: '',
    },
  })
  return result.isConfirmed
}

export interface PromptOptions {
  title: string
  value: string
  confirmText: string
  cancelText: string
  /** Shown when the field is submitted empty. */
  emptyText: string
}

/**
 * Asks the user for one line of text. Returns the trimmed answer, or null when
 * they cancel, so a caller can tell "left as it was" from "cleared".
 */
export async function promptDialog(options: PromptOptions): Promise<string | null> {
  const result = await Swal.fire({
    ...shared,
    input: 'text',
    title: options.title,
    inputValue: options.value,
    confirmButtonText: options.confirmText,
    cancelButtonText: options.cancelText,
    // The field, not the button, is what the user came here to fill in.
    focusConfirm: false,
    inputValidator: (value) => (value.trim() ? null : options.emptyText),
    customClass: { confirmButton: 'primary', cancelButton: '' },
  })
  if (!result.isConfirmed || typeof result.value !== 'string') {
    return null
  }
  const text = result.value.trim()
  return text ? text : null
}
