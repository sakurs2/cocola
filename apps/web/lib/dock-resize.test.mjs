import assert from "node:assert/strict";
import test from "node:test";

import { beginDockResize } from "./dock-resize.mjs";

class PointerCaptureTarget extends EventTarget {
  capturedPointers = new Set();

  setPointerCapture(pointerID) {
    this.capturedPointers.add(pointerID);
  }

  hasPointerCapture(pointerID) {
    return this.capturedPointers.has(pointerID);
  }

  releasePointerCapture(pointerID) {
    if (!this.capturedPointers.delete(pointerID)) return;
    this.dispatchEvent(pointerEvent("lostpointercapture", pointerID));
  }
}

function pointerEvent(type, pointerID, clientX = 0) {
  const event = new Event(type);
  Object.defineProperties(event, {
    pointerId: { value: pointerID },
    clientX: { value: clientX },
  });
  return event;
}

function startResize({ currentWidth = 760 } = {}) {
  const windowTarget = new EventTarget();
  windowTarget.innerWidth = 1600;
  const captureTarget = new PointerCaptureTarget();
  const bodyStyle = { cursor: "default", userSelect: "text" };
  const widths = [];
  const cleanup = beginDockResize({
    event: {
      button: 0,
      isPrimary: true,
      pointerId: 7,
      clientX: 300,
      currentTarget: captureTarget,
      preventDefault() {},
    },
    currentWidth,
    minWidth: 480,
    setWidth: (width) => widths.push(width),
    windowTarget,
    bodyStyle,
  });
  return { bodyStyle, captureTarget, cleanup, widths, windowTarget };
}

test("dock resize keeps the pointer captured while shrinking across an iframe", () => {
  const { bodyStyle, captureTarget, widths, windowTarget } = startResize();

  assert.equal(captureTarget.hasPointerCapture(7), true);
  windowTarget.dispatchEvent(pointerEvent("pointermove", 7, 420));
  assert.deepEqual(widths, [640]);

  windowTarget.dispatchEvent(pointerEvent("pointerup", 7, 420));
  assert.equal(captureTarget.hasPointerCapture(7), false);
  assert.deepEqual(bodyStyle, { cursor: "default", userSelect: "text" });
});

test("dock resize restores global styles when pointer completion is lost", () => {
  const { bodyStyle, captureTarget, cleanup, widths, windowTarget } = startResize({
    currentWidth: 620,
  });

  assert.deepEqual(bodyStyle, { cursor: "col-resize", userSelect: "none" });
  windowTarget.dispatchEvent(new Event("blur"));
  assert.deepEqual(bodyStyle, { cursor: "default", userSelect: "text" });
  assert.equal(captureTarget.hasPointerCapture(7), false);

  windowTarget.dispatchEvent(pointerEvent("pointermove", 7, 260));
  assert.deepEqual(widths, []);
  assert.doesNotThrow(() => cleanup?.());
});

test("dock resize cleans up when pointer capture is cancelled", () => {
  const { bodyStyle, captureTarget } = startResize();

  captureTarget.releasePointerCapture(7);
  assert.deepEqual(bodyStyle, { cursor: "default", userSelect: "text" });
});
