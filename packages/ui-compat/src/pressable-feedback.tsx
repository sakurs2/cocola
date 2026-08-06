"use client";

import {
  createElement,
  forwardRef,
  type ComponentPropsWithRef,
  type JSX,
  type ReactElement,
  type ReactNode,
} from "react";

type RenderFunction<E extends keyof JSX.IntrinsicElements> = (
  props: JSX.IntrinsicElements[E],
) => ReactElement;

export type PressableFeedbackRootProps<E extends keyof JSX.IntrinsicElements = "button"> = {
  children: ReactNode;
  className?: string;
  isDisabled?: boolean;
  render?: RenderFunction<E>;
};

export type PressableFeedbackHighlightProps = ComponentPropsWithRef<"div">;
export type PressableFeedbackRippleProps = ComponentPropsWithRef<"span">;
export type PressableFeedbackHoldConfirmProps = ComponentPropsWithRef<"span">;
export type PressableFeedbackProgressFeedbackProps = ComponentPropsWithRef<"span">;

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export function PressableFeedbackRoot<E extends keyof JSX.IntrinsicElements = "button">({
  children,
  className,
  isDisabled,
  render,
  ...props
}: PressableFeedbackRootProps<E> &
  Omit<JSX.IntrinsicElements[E], keyof PressableFeedbackRootProps<E>>) {
  const rootProps = {
    ...props,
    className: mergeClassNames("pressable-feedback", className),
    "data-disabled": isDisabled || undefined,
    "data-slot": "pressable-feedback",
  } as unknown as JSX.IntrinsicElements[E];

  if (render) {
    return render(rootProps);
  }

  return createElement("button", rootProps as unknown as JSX.IntrinsicElements["button"], children);
}

export const PressableFeedbackHighlight = forwardRef<
  HTMLDivElement,
  PressableFeedbackHighlightProps
>(function PressableFeedbackHighlight({ className, ...props }, ref) {
  return (
    <div
      {...props}
      ref={ref}
      aria-hidden="true"
      className={mergeClassNames("pressable-feedback__highlight", className)}
      data-slot="pressable-feedback-highlight"
    />
  );
});

function feedbackSlot(slot: string, displayName: string) {
  const Component = forwardRef<HTMLSpanElement, ComponentPropsWithRef<"span">>(
    function FeedbackSlot({ className, ...props }, ref) {
      return (
        <span
          {...props}
          ref={ref}
          className={mergeClassNames(`pressable-feedback__${slot}`, className)}
          data-slot={`pressable-feedback-${slot}`}
        />
      );
    },
  );
  Component.displayName = displayName;
  return Component;
}

export const PressableFeedbackRipple = feedbackSlot("ripple", "PressableFeedbackRipple");
export const PressableFeedbackHoldConfirm = feedbackSlot(
  "hold-confirm",
  "PressableFeedbackHoldConfirm",
);
export const PressableFeedbackProgressFeedback = feedbackSlot(
  "progress-feedback",
  "PressableFeedbackProgressFeedback",
);

export const PressableFeedback = Object.assign(PressableFeedbackRoot, {
  Root: PressableFeedbackRoot,
  Highlight: PressableFeedbackHighlight,
  Ripple: PressableFeedbackRipple,
  HoldConfirm: PressableFeedbackHoldConfirm,
  ProgressFeedback: PressableFeedbackProgressFeedback,
});
