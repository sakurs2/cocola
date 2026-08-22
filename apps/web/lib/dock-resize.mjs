/**
 * Start resizing the workspace dock.
 *
 * Pointer capture is required because the dock may contain an iframe. Without
 * it, moving into the preview transfers pointer events to the iframe and leaves
 * the parent page stuck in its resizing state.
 *
 * @param {{
 *   event: {
 *     button: number;
 *     isPrimary: boolean;
 *     pointerId: number;
 *     clientX: number;
 *     currentTarget: HTMLElement;
 *     preventDefault: () => void;
 *   };
 *   currentWidth: number;
 *   minWidth: number;
 *   setWidth: (width: number) => void;
 *   windowTarget?: Window;
 *   bodyStyle?: CSSStyleDeclaration;
 * }} options
 * @returns {(() => void) | undefined}
 */
export function beginDockResize({
  event,
  currentWidth,
  minWidth,
  setWidth,
  windowTarget = window,
  bodyStyle = document.body.style,
}) {
  if (event.button !== 0 || !event.isPrimary) return undefined;

  event.preventDefault();
  const captureTarget = event.currentTarget;
  const pointerID = event.pointerId;
  const startX = event.clientX;
  const previousCursor = bodyStyle.cursor;
  const previousUserSelect = bodyStyle.userSelect;
  const maxWidth = Math.max(minWidth, Math.min(windowTarget.innerWidth * 0.62, 760));
  let active = true;

  const onPointerMove = (moveEvent) => {
    if (moveEvent.pointerId !== pointerID) return;
    const nextWidth = currentWidth - (moveEvent.clientX - startX);
    setWidth(Math.min(Math.max(nextWidth, minWidth), maxWidth));
  };

  const finish = () => {
    if (!active) return;
    active = false;
    bodyStyle.cursor = previousCursor;
    bodyStyle.userSelect = previousUserSelect;
    windowTarget.removeEventListener("pointermove", onPointerMove);
    windowTarget.removeEventListener("pointerup", onPointerEnd);
    windowTarget.removeEventListener("pointercancel", onPointerEnd);
    windowTarget.removeEventListener("blur", finish);
    captureTarget.removeEventListener("lostpointercapture", onLostPointerCapture);
    if (captureTarget.hasPointerCapture(pointerID)) {
      captureTarget.releasePointerCapture(pointerID);
    }
  };

  const onPointerEnd = (endEvent) => {
    if (endEvent.pointerId === pointerID) finish();
  };

  const onLostPointerCapture = (lostEvent) => {
    if (lostEvent.pointerId === pointerID) finish();
  };

  captureTarget.setPointerCapture(pointerID);
  bodyStyle.cursor = "col-resize";
  bodyStyle.userSelect = "none";
  windowTarget.addEventListener("pointermove", onPointerMove);
  windowTarget.addEventListener("pointerup", onPointerEnd);
  windowTarget.addEventListener("pointercancel", onPointerEnd);
  windowTarget.addEventListener("blur", finish);
  captureTarget.addEventListener("lostpointercapture", onLostPointerCapture);

  return finish;
}
