"use client";

import { Button, Tooltip } from "@heroui/react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithRef,
  type ReactNode,
  type Ref,
} from "react";

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

function setRef<T>(ref: Ref<T> | undefined, value: T | null) {
  if (typeof ref === "function") ref(value);
  else if (ref) (ref as React.MutableRefObject<T | null>).current = value;
}

type ScrollBehaviorMode = "instant" | "smooth";
type ScrollState = { hasOverflow: boolean; isAtBottom: boolean };
type MeasureOptions = { preserveAtBottom?: boolean };

type ChatConversationContextValue = ScrollState & {
  measureScrollState: (options?: MeasureOptions) => void;
  resize: ScrollBehaviorMode;
  scrollToBottom: (behavior?: ScrollBehaviorMode) => void;
};

const ChatConversationContext = createContext<ChatConversationContextValue | null>(null);

function hasOverflow(element: HTMLElement) {
  return element.scrollHeight - element.clientHeight > 4;
}

function isAtBottom(element: HTMLElement) {
  return (
    !hasOverflow(element) || element.scrollHeight - element.scrollTop - element.clientHeight <= 4
  );
}

function measure(element: HTMLElement): ScrollState {
  return { hasOverflow: hasOverflow(element), isAtBottom: isAtBottom(element) };
}

export interface ChatConversationRootProps extends Omit<ComponentPropsWithRef<"div">, "resize"> {
  children: ReactNode;
  initial?: ScrollBehaviorMode;
  resize?: ScrollBehaviorMode;
}

export function ChatConversationRoot({
  children,
  className,
  initial = "smooth",
  onScroll,
  ref,
  resize = "smooth",
  ...props
}: ChatConversationRootProps) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const frameRef = useRef<number | null>(null);
  const lastScrollTop = useRef(0);
  const programmaticScroll = useRef(false);
  const stateRef = useRef<ScrollState>({ hasOverflow: false, isAtBottom: true });
  const [scrollState, setScrollState] = useState(stateRef.current);
  const updateState = useCallback((next: ScrollState) => {
    stateRef.current = next;
    setScrollState((current) =>
      current.hasOverflow === next.hasOverflow && current.isAtBottom === next.isAtBottom
        ? current
        : next,
    );
  }, []);
  const measureScrollState = useCallback(
    ({ preserveAtBottom = false }: MeasureOptions = {}) => {
      const element = rootRef.current;
      if (!element) return;
      const next = measure(element);
      updateState({
        hasOverflow: next.hasOverflow,
        isAtBottom: (preserveAtBottom && stateRef.current.isAtBottom) || next.isAtBottom,
      });
    },
    [updateState],
  );
  const scrollToBottom = useCallback(
    (behavior: ScrollBehaviorMode = resize) => {
      const element = rootRef.current;
      if (!element) return;
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      programmaticScroll.current = true;
      frameRef.current = requestAnimationFrame(() => {
        frameRef.current = null;
        element.scrollTo({
          behavior: behavior === "smooth" ? "smooth" : "auto",
          top: element.scrollHeight,
        });
      });
    },
    [resize],
  );

  useEffect(
    () => () => {
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
    },
    [],
  );

  useLayoutEffect(() => {
    lastScrollTop.current = rootRef.current?.scrollTop ?? 0;
    measureScrollState({ preserveAtBottom: true });
    scrollToBottom(initial);
  }, [initial, measureScrollState, scrollToBottom]);

  useEffect(() => {
    const element = rootRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      const shouldFollow = stateRef.current.isAtBottom;
      measureScrollState({ preserveAtBottom: true });
      if (shouldFollow) scrollToBottom(resize);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [measureScrollState, resize, scrollToBottom]);

  const context = useMemo(
    () => ({ ...scrollState, measureScrollState, resize, scrollToBottom }),
    [measureScrollState, resize, scrollState, scrollToBottom],
  );

  return (
    <ChatConversationContext.Provider value={context}>
      <div
        ref={(element) => {
          rootRef.current = element;
          setRef(ref, element);
        }}
        className={mergeClassNames("chat-conversation", className)}
        data-slot="chat-conversation"
        role="log"
        onScroll={(event) => {
          onScroll?.(event);
          const element = event.currentTarget;
          const scrollingUp = element.scrollTop < lastScrollTop.current - 1;
          lastScrollTop.current = element.scrollTop;
          if (programmaticScroll.current && !scrollingUp) {
            const bottom = isAtBottom(element);
            if (bottom) programmaticScroll.current = false;
            updateState({
              hasOverflow: hasOverflow(element),
              isAtBottom: bottom || stateRef.current.isAtBottom,
            });
            return;
          }
          programmaticScroll.current = false;
          updateState(measure(element));
        }}
        {...props}
      >
        {children}
      </div>
    </ChatConversationContext.Provider>
  );
}

export interface ChatConversationContentProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function ChatConversationContent({
  children,
  className,
  ref,
  ...props
}: ChatConversationContentProps) {
  const context = useContext(ChatConversationContext);
  const contentRef = useRef<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    const wasAtBottom = context?.isAtBottom ?? true;
    context?.measureScrollState({ preserveAtBottom: wasAtBottom });
    if (wasAtBottom) context?.scrollToBottom(context.resize);
  }, [context]);
  useEffect(() => {
    const element = contentRef.current;
    if (!element || !context || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      const wasAtBottom = context.isAtBottom;
      context.measureScrollState({ preserveAtBottom: wasAtBottom });
      if (wasAtBottom) context.scrollToBottom(context.resize);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [context]);
  return (
    <div
      ref={(element) => {
        contentRef.current = element;
        setRef(ref, element);
      }}
      className={mergeClassNames("chat-conversation__content", className)}
      data-slot="chat-conversation-content"
      {...props}
    >
      {children}
    </div>
  );
}

export function ChatConversationScrollAnchor({
  className,
  ...props
}: ComponentPropsWithRef<"div">) {
  return (
    <div
      aria-hidden="true"
      className={mergeClassNames("chat-conversation__scroll-anchor", className)}
      data-slot="chat-conversation-scroll-anchor"
      {...props}
    />
  );
}

export interface ChatConversationScrollButtonProps extends Omit<
  ComponentPropsWithRef<typeof Button>,
  "className"
> {
  "aria-label"?: string;
  className?: string;
  tooltip?: ReactNode;
}

export function ChatConversationScrollButton({
  "aria-label": ariaLabel,
  className,
  isDisabled,
  tooltip,
  ...props
}: ChatConversationScrollButtonProps) {
  const context = useContext(ChatConversationContext);
  if (!context) return null;
  const visible = context.hasOverflow && !context.isAtBottom;
  const button = (
    <Button
      {...props}
      isIconOnly
      aria-label={ariaLabel}
      aria-hidden={!visible || undefined}
      className={mergeClassNames("chat-conversation__scroll-button", className)}
      data-slot="chat-conversation-scroll-button"
      isDisabled={!visible || isDisabled}
      size="sm"
      variant="secondary"
      onPress={() => context.scrollToBottom("smooth")}
    >
      <svg aria-hidden="true" className="size-4" viewBox="0 0 16 16" fill="none">
        <path
          d="M2.97 5.47a.75.75 0 0 1 1.06 0L8 9.44l3.97-3.97a.75.75 0 1 1 1.06 1.06l-4.5 4.5a.75.75 0 0 1-1.06 0l-4.5-4.5a.75.75 0 0 1 0-1.06"
          fill="currentColor"
          fillRule="evenodd"
        />
      </svg>
    </Button>
  );
  return (
    <div
      aria-hidden={!visible || undefined}
      className="chat-conversation__scroll-button-container"
      data-slot="chat-conversation-scroll-button-container"
      data-state={visible ? "visible" : "hidden"}
    >
      {tooltip ? (
        <Tooltip delay={0}>
          <Tooltip.Trigger>{button}</Tooltip.Trigger>
          <Tooltip.Content placement="top">{tooltip}</Tooltip.Content>
        </Tooltip>
      ) : (
        button
      )}
    </div>
  );
}

export const ChatConversation = Object.assign(ChatConversationRoot, {
  Content: ChatConversationContent,
  Root: ChatConversationRoot,
  ScrollAnchor: ChatConversationScrollAnchor,
  ScrollButton: ChatConversationScrollButton,
});
