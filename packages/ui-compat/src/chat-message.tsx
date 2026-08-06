"use client";

import { Avatar, Button, Tooltip } from "@heroui/react";
import { createContext, useContext, type ComponentPropsWithRef, type ReactNode } from "react";

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

const ChatMessageContext = createContext(false);

type ChatMessageSlotProps = ComponentPropsWithRef<"div"> & { children: ReactNode };

export function ChatMessageUser({ children, className, ...props }: ChatMessageSlotProps) {
  return (
    <ChatMessageContext.Provider value>
      <div
        className={mergeClassNames("chat-message--user", className)}
        data-slot="chat-message-user"
        {...props}
      >
        {children}
      </div>
    </ChatMessageContext.Provider>
  );
}

export function ChatMessageAssistant({ children, className, ...props }: ChatMessageSlotProps) {
  return (
    <ChatMessageContext.Provider value>
      <div
        className={mergeClassNames("chat-message--assistant", className)}
        data-slot="chat-message-assistant"
        {...props}
      >
        {children}
      </div>
    </ChatMessageContext.Provider>
  );
}

function ChatMessageSlot({
  children,
  className,
  slot,
  ...props
}: ChatMessageSlotProps & { slot: string }) {
  useContext(ChatMessageContext);
  return (
    <div
      className={mergeClassNames(`chat-message__${slot}`, className)}
      data-slot={`chat-message-${slot}`}
      {...props}
    >
      {children}
    </div>
  );
}

export function ChatMessageBubble(props: ChatMessageSlotProps) {
  return <ChatMessageSlot {...props} slot="bubble" />;
}

export function ChatMessageContent(props: ChatMessageSlotProps) {
  return <ChatMessageSlot {...props} slot="content" />;
}

export function ChatMessageMedia(props: ChatMessageSlotProps) {
  return <ChatMessageSlot {...props} slot="media" />;
}

export function ChatMessageActions(props: ChatMessageSlotProps) {
  return <ChatMessageSlot {...props} slot="actions" />;
}

export function ChatMessageBody(props: ChatMessageSlotProps) {
  return <ChatMessageSlot {...props} slot="body" />;
}

export interface ChatMessageActionProps extends Omit<
  ComponentPropsWithRef<typeof Button>,
  "className"
> {
  "aria-label"?: string;
  children?: ReactNode;
  className?: string;
  tooltip?: ReactNode;
}

export function ChatMessageAction({
  "aria-label": ariaLabel,
  children,
  className,
  tooltip,
  ...props
}: ChatMessageActionProps) {
  const action = (
    <Button
      {...props}
      isIconOnly
      aria-label={ariaLabel}
      className={mergeClassNames("chat-message__action", className)}
      data-slot="chat-message-action"
      size="sm"
      variant="ghost"
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

export interface ChatMessageAvatarProps extends Omit<ComponentPropsWithRef<"div">, "children"> {
  alt: string;
  fallback?: string;
  show?: boolean;
  src?: string;
}

export function ChatMessageAvatar({
  alt,
  className,
  fallback,
  show = true,
  src,
  ...props
}: ChatMessageAvatarProps) {
  if (!show) {
    return (
      <div
        aria-hidden="true"
        className={mergeClassNames("chat-message__avatar-spacer", className)}
        data-slot="chat-message-avatar-spacer"
        {...props}
      />
    );
  }
  return (
    <div
      className={mergeClassNames("chat-message__avatar", className)}
      data-slot="chat-message-avatar"
      {...props}
    >
      <Avatar className="size-8">
        {src ? <Avatar.Image alt={alt} src={src} /> : null}
        {fallback ? <Avatar.Fallback>{fallback}</Avatar.Fallback> : null}
      </Avatar>
    </div>
  );
}

export const ChatMessage = {
  Action: ChatMessageAction,
  Actions: ChatMessageActions,
  Assistant: ChatMessageAssistant,
  Avatar: ChatMessageAvatar,
  Body: ChatMessageBody,
  Bubble: ChatMessageBubble,
  Content: ChatMessageContent,
  Media: ChatMessageMedia,
  User: ChatMessageUser,
};
