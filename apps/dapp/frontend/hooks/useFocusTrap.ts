import { useEffect, RefObject } from "react";

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

interface FocusTrapOptions {
  /**
   * Called when Escape is pressed. Wiring this here rather than in each
   * modal is what guarantees Escape cancels rather than submits: the handler
   * runs on keydown and stops the event before it can reach a form.
   */
  onEscape?: () => void;
}

/**
 * Traps focus inside a container while it is active, and returns focus to
 * whatever was focused before it opened.
 *
 * Focus restoration is the half that is easy to leave out and the half a
 * keyboard user actually notices: without it, closing a modal drops focus onto
 * document.body and the next Tab starts again from the top of the page,
 * stranding the user far from the control they opened the modal with
 * (nester#1128).
 */
export function useFocusTrap(
  ref: RefObject<HTMLElement | null>,
  isActive: boolean,
  options: FocusTrapOptions = {},
) {
  const { onEscape } = options;

  useEffect(() => {
    const container = ref.current;
    if (!isActive || !container) return;

    // Captured before focus moves into the modal so it can be handed back
    // on close.
    const previouslyFocused = document.activeElement as HTMLElement | null;

    // Re-read on each keypress rather than once on open: the money-path
    // modals swap their controls as the transaction advances through
    // review, signing and confirmation, so a list captured at open time
    // goes stale immediately.
    const focusable = () =>
      Array.from(
        container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      ).filter(
        (el) => el.offsetParent !== null || el === document.activeElement,
      );

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && onEscape) {
        // stopPropagation as well as preventDefault: Escape must cancel
        // the modal, never reach a form underneath it.
        e.preventDefault();
        e.stopPropagation();
        onEscape();
        return;
      }

      if (e.key !== "Tab") return;

      const elements = focusable();
      if (elements.length === 0) {
        // Nothing focusable inside: keep focus in the container rather
        // than letting Tab escape to the page behind.
        e.preventDefault();
        return;
      }

      const first = elements[0];
      const last = elements[elements.length - 1];
      const active = document.activeElement;

      if (e.shiftKey && (active === first || !container.contains(active))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    };

    // Listen on the document, not the container: focus can sit on the
    // container itself or briefly on body during transitions, and a
    // container-scoped listener misses those keystrokes.
    document.addEventListener("keydown", handleKeyDown, true);

    const initial = focusable()[0] ?? container;
    initial.focus();

    return () => {
      document.removeEventListener("keydown", handleKeyDown, true);
      // Only restore if focus is still inside the closing modal;
      // otherwise something else has deliberately claimed it.
      if (
        previouslyFocused &&
        (!document.activeElement || container.contains(document.activeElement))
      ) {
        previouslyFocused.focus();
      }
    };
  }, [ref, isActive, onEscape]);
}
