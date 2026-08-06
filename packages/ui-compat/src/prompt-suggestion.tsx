"use client";

import { Card } from "@heroui/react";
import { Button as AriaButton, type ButtonProps as AriaButtonProps } from "react-aria-components";
import { createContext, useContext, type ComponentPropsWithRef, type ReactNode } from "react";

export type PromptSuggestionVariant = "pill" | "card";

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

const PromptSuggestionContext = createContext<PromptSuggestionVariant>("pill");

export interface PromptSuggestionRootProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
  variant?: PromptSuggestionVariant;
}

export function PromptSuggestionRoot({
  children,
  className,
  variant = "pill",
  ...props
}: PromptSuggestionRootProps) {
  return (
    <PromptSuggestionContext.Provider value={variant}>
      <div
        className={mergeClassNames("prompt-suggestion", `prompt-suggestion--${variant}`, className)}
        data-slot="prompt-suggestion"
        {...props}
      >
        {children}
      </div>
    </PromptSuggestionContext.Provider>
  );
}

type SlotProps<T extends keyof React.JSX.IntrinsicElements> = ComponentPropsWithRef<T> & {
  children: ReactNode;
};

export function PromptSuggestionHeader({ children, className, ...props }: SlotProps<"div">) {
  return (
    <div
      className={mergeClassNames("prompt-suggestion__header", className)}
      data-slot="prompt-suggestion-header"
      {...props}
    >
      {children}
    </div>
  );
}

export function PromptSuggestionTitle({ children, className, ...props }: SlotProps<"h2">) {
  return (
    <h2
      className={mergeClassNames("prompt-suggestion__title", className)}
      data-slot="prompt-suggestion-title"
      {...props}
    >
      {children}
    </h2>
  );
}

export function PromptSuggestionDescription({ children, className, ...props }: SlotProps<"p">) {
  return (
    <p
      className={mergeClassNames("prompt-suggestion__description", className)}
      data-slot="prompt-suggestion-description"
      {...props}
    >
      {children}
    </p>
  );
}

export interface PromptSuggestionGroupProps extends ComponentPropsWithRef<"section"> {
  children: ReactNode;
  description?: ReactNode;
  label?: ReactNode;
}

export function PromptSuggestionGroup({
  children,
  className,
  description,
  label,
  ...props
}: PromptSuggestionGroupProps) {
  return (
    <section
      className={mergeClassNames("prompt-suggestion__group", className)}
      data-slot="prompt-suggestion-group"
      {...props}
    >
      {label || description ? (
        <div className="flex flex-col gap-1">
          {label ? <h3 className="prompt-suggestion__group-label">{label}</h3> : null}
          {description ? (
            <p className="prompt-suggestion__group-description">{description}</p>
          ) : null}
        </div>
      ) : null}
      {children}
    </section>
  );
}

export function PromptSuggestionItems({ children, className, ...props }: SlotProps<"div">) {
  const variant = useContext(PromptSuggestionContext);
  return (
    <div
      className={mergeClassNames(
        "prompt-suggestion__items",
        `prompt-suggestion__items--${variant}`,
        className,
      )}
      data-slot="prompt-suggestion-items"
      {...props}
    >
      {children}
    </div>
  );
}

export interface PromptSuggestionItemProps extends Omit<AriaButtonProps, "className"> {
  children: ReactNode;
  className?: string;
  showEndIcon?: boolean;
}

export function PromptSuggestionItem({
  children,
  className,
  showEndIcon = true,
  ...props
}: PromptSuggestionItemProps) {
  const variant = useContext(PromptSuggestionContext);
  if (variant === "card") {
    return (
      <Card
        className={mergeClassNames(
          "prompt-suggestion__item prompt-suggestion__item--card",
          className,
        )}
        data-slot="prompt-suggestion-item"
        {...(props as unknown as ComponentPropsWithRef<"div">)}
      >
        {children}
      </Card>
    );
  }
  return (
    <AriaButton
      className={mergeClassNames(
        "prompt-suggestion__item prompt-suggestion__item--pill",
        className,
      )}
      data-slot="prompt-suggestion-item"
      {...props}
    >
      <span className="prompt-suggestion__item-label">{children}</span>
      {showEndIcon ? (
        <svg
          aria-hidden="true"
          className="prompt-suggestion__item-end-icon"
          viewBox="0 0 16 16"
          fill="none"
        >
          <path
            d="M5.47 13.03a.75.75 0 0 1 0-1.06L9.44 8 5.47 4.03a.75.75 0 0 1 1.06-1.06l4.5 4.5a.75.75 0 0 1 0 1.06l-4.5 4.5a.75.75 0 0 1-1.06 0"
            fill="currentColor"
            fillRule="evenodd"
          />
        </svg>
      ) : null}
    </AriaButton>
  );
}

export function PromptSuggestionItemTitle({
  children,
  className,
  ...props
}: ComponentPropsWithRef<typeof Card.Title>) {
  return (
    <Card.Title data-slot="prompt-suggestion-item-title" className={className} {...props}>
      {children}
    </Card.Title>
  );
}

export function PromptSuggestionItemDescription({
  children,
  className,
  ...props
}: ComponentPropsWithRef<typeof Card.Description>) {
  return (
    <Card.Description
      className={mergeClassNames("prompt-suggestion__item-description", className)}
      {...props}
    >
      {children}
    </Card.Description>
  );
}

export function PromptSuggestionItemFooter({
  children,
  className,
  ...props
}: ComponentPropsWithRef<typeof Card.Footer>) {
  return (
    <Card.Footer
      className={mergeClassNames("prompt-suggestion__item-footer", className)}
      data-slot="prompt-suggestion-item-footer"
      {...props}
    >
      {children}
    </Card.Footer>
  );
}

export function PromptSuggestionItemTags({ children, className, ...props }: SlotProps<"div">) {
  return (
    <div
      className={mergeClassNames("prompt-suggestion__item-tags", className)}
      data-slot="prompt-suggestion-item-tags"
      {...props}
    >
      {children}
    </div>
  );
}

export function PromptSuggestionItemMeta({ children, className, ...props }: SlotProps<"span">) {
  return (
    <span
      className={mergeClassNames("prompt-suggestion__item-meta", className)}
      data-slot="prompt-suggestion-item-meta"
      {...props}
    >
      {children}
    </span>
  );
}

export const PromptSuggestion = Object.assign(PromptSuggestionRoot, {
  Description: PromptSuggestionDescription,
  Group: PromptSuggestionGroup,
  Header: PromptSuggestionHeader,
  Item: PromptSuggestionItem,
  ItemDescription: PromptSuggestionItemDescription,
  ItemFooter: PromptSuggestionItemFooter,
  ItemMeta: PromptSuggestionItemMeta,
  ItemTags: PromptSuggestionItemTags,
  ItemTitle: PromptSuggestionItemTitle,
  Items: PromptSuggestionItems,
  Root: PromptSuggestionRoot,
  Title: PromptSuggestionTitle,
});
