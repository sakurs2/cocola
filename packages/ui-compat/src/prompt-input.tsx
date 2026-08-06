"use client";

import { Button, Spinner, TextArea, Tooltip } from "@heroui/react";
import {
  createContext,
  useContext,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithRef,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from "react";

export type PromptInputStatus = "ready" | "submitted" | "streaming" | "error";
export type PromptInputLayout = "stacked" | "compact" | "inline";
export type PromptInputSize = "sm" | "md" | "lg";
export type PromptInputVariant = "primary" | "secondary";

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

function isGenerating(status: PromptInputStatus) {
  return status === "submitted" || status === "streaming";
}

type PromptInputContextValue = {
  allowSubmitWhileRunning: boolean;
  disabled: boolean;
  layout: PromptInputLayout;
  lockInputOnRun: boolean;
  maxHeight: number | string;
  onStop?: () => void;
  onSubmit?: () => void;
  setValue: (value: string) => void;
  status: PromptInputStatus;
  textareaRef: React.MutableRefObject<HTMLTextAreaElement | null>;
  value: string;
  variant: PromptInputVariant;
};

const PromptInputContext = createContext<PromptInputContextValue | null>(null);

function usePromptInput() {
  const context = useContext(PromptInputContext);
  if (!context) throw new Error("PromptInput components must be rendered inside PromptInput.Root");
  return context;
}

export interface PromptInputRootProps extends Omit<ComponentPropsWithRef<"div">, "onSubmit"> {
  allowSubmitWhileRunning?: boolean;
  children: ReactNode;
  isDisabled?: boolean;
  isPending?: boolean;
  layout?: PromptInputLayout;
  lockInputOnRun?: boolean;
  maxHeight?: number | string;
  onStop?: () => void;
  onSubmit?: () => void;
  onValueChange?: (value: string) => void;
  size?: PromptInputSize;
  status?: PromptInputStatus;
  value?: string;
  variant?: PromptInputVariant;
}

export function PromptInputRoot({
  allowSubmitWhileRunning = false,
  children,
  className,
  isDisabled = false,
  isPending = false,
  layout = "stacked",
  lockInputOnRun = true,
  maxHeight = 240,
  onStop,
  onSubmit,
  onValueChange,
  size = "md",
  status: statusProp,
  value: valueProp,
  variant = "primary",
  ...props
}: PromptInputRootProps) {
  const [uncontrolledValue, setUncontrolledValue] = useState(valueProp ?? "");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const value = valueProp ?? uncontrolledValue;
  const status = statusProp ?? (isPending ? "streaming" : "ready");
  const setValue = (nextValue: string) => {
    if (valueProp === undefined) setUncontrolledValue(nextValue);
    onValueChange?.(nextValue);
  };
  const context = useMemo(
    () => ({
      allowSubmitWhileRunning,
      disabled: isDisabled,
      layout,
      lockInputOnRun,
      maxHeight,
      onStop,
      onSubmit,
      setValue,
      status,
      textareaRef,
      value,
      variant,
    }),
    [
      allowSubmitWhileRunning,
      isDisabled,
      layout,
      lockInputOnRun,
      maxHeight,
      onStop,
      onSubmit,
      status,
      value,
      variant,
    ],
  );

  return (
    <PromptInputContext.Provider value={context}>
      <div
        className={mergeClassNames(
          "prompt-input",
          size === "sm" ? "prompt-input--sm" : undefined,
          size === "lg" ? "prompt-input--lg" : undefined,
          className,
        )}
        data-disabled={isDisabled || undefined}
        data-layout={layout}
        data-pending={isGenerating(status) || undefined}
        data-slot="prompt-input"
        data-status={status}
        data-variant={variant}
        {...props}
      >
        {children}
      </div>
    </PromptInputContext.Provider>
  );
}

export interface PromptInputShellProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputShell({
  children,
  className,
  onClick,
  ...props
}: PromptInputShellProps) {
  const { disabled, textareaRef, variant } = usePromptInput();
  return (
    <div
      className={mergeClassNames(
        "prompt-input__shell",
        `prompt-input__shell--${variant}`,
        className,
      )}
      data-slot="prompt-input-shell"
      onClick={(event) => {
        onClick?.(event);
        if (
          disabled ||
          event.defaultPrevented ||
          (event.target as Element).closest(
            'button, a, input, select, textarea, [role="button"], [data-slot="prompt-input-toolbar"]',
          )
        ) {
          return;
        }
        textareaRef.current?.focus();
      }}
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputContentProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputContent({ children, className, ...props }: PromptInputContentProps) {
  return (
    <div
      className={mergeClassNames("prompt-input__content", className)}
      data-slot="prompt-input-content"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputAttachmentsProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputAttachments({
  children,
  className,
  ...props
}: PromptInputAttachmentsProps) {
  return (
    <div
      className={mergeClassNames("prompt-input__attachments", className)}
      data-slot="prompt-input-attachments"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputTextAreaProps extends Omit<
  ComponentPropsWithRef<typeof TextArea>,
  "className"
> {
  className?: string;
  disableAutosize?: boolean;
}

export function PromptInputTextArea({
  className,
  disableAutosize = false,
  onChange,
  onKeyDown,
  ref,
  ...props
}: PromptInputTextAreaProps) {
  const {
    allowSubmitWhileRunning,
    disabled,
    lockInputOnRun,
    maxHeight,
    onSubmit,
    setValue,
    status,
    textareaRef,
    value,
  } = usePromptInput();
  const generating = isGenerating(status);
  const setRefs = (element: HTMLTextAreaElement | null) => {
    textareaRef.current = element;
    if (typeof ref === "function") ref(element);
    else if (ref) (ref as React.MutableRefObject<HTMLTextAreaElement | null>).current = element;
  };
  const resize = (element: HTMLTextAreaElement) => {
    if (disableAutosize) return;
    element.style.height = "auto";
    const limit = typeof maxHeight === "number" ? `${maxHeight}px` : maxHeight;
    element.style.height = `min(${element.scrollHeight}px, ${limit})`;
  };

  return (
    <TextArea
      {...props}
      ref={setRefs as Ref<HTMLTextAreaElement>}
      fullWidth
      aria-label={props["aria-label"] ?? "Message input"}
      className={mergeClassNames("prompt-input__textarea", className)}
      data-slot="prompt-input-textarea"
      disabled={disabled || (lockInputOnRun && generating)}
      placeholder={props.placeholder ?? "What do you want to know?"}
      rows={1}
      value={value}
      onChange={(event) => {
        setValue(event.target.value);
        resize(event.target);
        onChange?.(event);
      }}
      onKeyDown={(event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === "Enter" && !event.shiftKey) {
          event.preventDefault();
          if (!generating || allowSubmitWhileRunning) onSubmit?.();
        }
        onKeyDown?.(event);
      }}
    />
  );
}

export interface PromptInputToolbarProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputToolbar({ children, className, ...props }: PromptInputToolbarProps) {
  return (
    <div
      className={mergeClassNames("prompt-input__toolbar", className)}
      data-slot="prompt-input-toolbar"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputToolbarStartProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputToolbarStart({
  children,
  className,
  ...props
}: PromptInputToolbarStartProps) {
  return (
    <div
      className={mergeClassNames("prompt-input__toolbar-start", className)}
      data-slot="prompt-input-toolbar-start"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputToolbarEndProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
}

export function PromptInputToolbarEnd({
  children,
  className,
  ...props
}: PromptInputToolbarEndProps) {
  return (
    <div
      className={mergeClassNames("prompt-input__toolbar-end", className)}
      data-slot="prompt-input-toolbar-end"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptInputFooterProps extends ComponentPropsWithRef<"p"> {
  children: ReactNode;
}

export function PromptInputFooter({ children, className, ...props }: PromptInputFooterProps) {
  return (
    <p
      className={mergeClassNames("prompt-input__footer", className)}
      data-slot="prompt-input-footer"
      {...props}
    >
      {children}
    </p>
  );
}

export interface PromptInputActionProps extends ComponentPropsWithRef<typeof Button> {
  children: ReactNode;
  tooltip?: ReactNode;
}

export function PromptInputAction({
  children,
  className,
  isDisabled,
  tooltip,
  variant = "tertiary",
  ...props
}: PromptInputActionProps) {
  const { disabled, layout, lockInputOnRun, status } = usePromptInput();
  const action = (
    <Button
      {...props}
      isIconOnly
      className={className}
      data-slot="prompt-input-action"
      isDisabled={disabled || isDisabled || (lockInputOnRun && isGenerating(status))}
      size={layout === "stacked" ? "md" : "sm"}
      variant={variant}
    >
      {children}
    </Button>
  );
  if (!tooltip) return action;
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger>{action}</Tooltip.Trigger>
      <Tooltip.Content>{tooltip}</Tooltip.Content>
    </Tooltip>
  );
}

export interface PromptInputSendProps extends Omit<
  ComponentPropsWithRef<typeof Button>,
  "className"
> {
  children?: ReactNode;
  className?: string;
  onStop?: () => void;
  status?: PromptInputStatus;
}

export function PromptInputSend({
  children,
  className,
  isDisabled,
  onPress,
  onStop: onStopProp,
  status: statusProp,
  ...props
}: PromptInputSendProps) {
  const context = usePromptInput();
  const status = statusProp ?? context.status;
  const generating = isGenerating(status);
  const onStop = onStopProp ?? context.onStop;
  const canStop = generating && Boolean(onStop);
  const disabled =
    isDisabled ??
    (context.disabled ||
      (generating && !canStop && !(context.allowSubmitWhileRunning && context.value.trim())) ||
      (!generating && !context.value.trim()));

  return (
    <Button
      {...props}
      isIconOnly
      aria-label={props["aria-label"] ?? (canStop ? "Stop" : "Send message")}
      className={mergeClassNames("prompt-input__send", className)}
      data-slot="prompt-input-send"
      data-status={status}
      isDisabled={disabled}
      size={context.layout === "stacked" ? "md" : "sm"}
      onPress={(event) => {
        if (canStop) onStop?.();
        else context.onSubmit?.();
        onPress?.(event);
      }}
    >
      {children ?? <PromptInputStatusIcon status={status} />}
    </Button>
  );
}

function PromptInputStatusIcon({ status }: { status: PromptInputStatus }) {
  if (status === "submitted") return <Spinner color="current" size="sm" />;
  if (status === "streaming") {
    return (
      <svg aria-hidden="true" className="size-4" viewBox="0 0 16 16" fill="none">
        <path
          d="M4.5 1.5a3 3 0 0 0-3 3v7a3 3 0 0 0 3 3h7a3 3 0 0 0 3-3v-7a3 3 0 0 0-3-3z"
          fill="currentColor"
          fillRule="evenodd"
        />
      </svg>
    );
  }
  if (status === "error") {
    return (
      <svg aria-hidden="true" className="size-4" viewBox="0 0 16 16" fill="none">
        <path
          d="M3.47 3.47a.75.75 0 0 1 1.06 0L8 6.94l3.47-3.47a.75.75 0 1 1 1.06 1.06L9.06 8l3.47 3.47a.75.75 0 1 1-1.06 1.06L8 9.06l-3.47 3.47a.75.75 0 0 1-1.06-1.06L6.94 8 3.47 4.53a.75.75 0 0 1 0-1.06"
          fill="currentColor"
          fillRule="evenodd"
        />
      </svg>
    );
  }
  return (
    <svg aria-hidden="true" className="size-4" viewBox="0 0 16 16" fill="none">
      <path
        d="M8 14.75a.75.75 0 0 1-.75-.75V3.81L4.53 6.53a.75.75 0 0 1-1.06-1.06l4-4a.75.75 0 0 1 1.06 0l4 4a.75.75 0 0 1-1.06 1.06L8.75 3.81V14a.75.75 0 0 1-.75.75"
        fill="currentColor"
        fillRule="evenodd"
      />
    </svg>
  );
}

export const PromptInput = Object.assign(PromptInputRoot, {
  Action: PromptInputAction,
  Attachments: PromptInputAttachments,
  Content: PromptInputContent,
  Footer: PromptInputFooter,
  Root: PromptInputRoot,
  Send: PromptInputSend,
  Shell: PromptInputShell,
  TextArea: PromptInputTextArea,
  Toolbar: PromptInputToolbar,
  ToolbarEnd: PromptInputToolbarEnd,
  ToolbarStart: PromptInputToolbarStart,
});
