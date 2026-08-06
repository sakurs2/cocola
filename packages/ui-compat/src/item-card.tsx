"use client";

import {
  createElement,
  forwardRef,
  type ComponentPropsWithRef,
  type JSX,
  type ReactElement,
  type ReactNode,
} from "react";

type ItemCardVariant = "default" | "outline" | "secondary" | "tertiary" | "transparent";

type RenderFunction<E extends keyof JSX.IntrinsicElements> = (
  props: JSX.IntrinsicElements[E],
) => ReactElement;

export type ItemCardRootProps<E extends keyof JSX.IntrinsicElements = "div"> = {
  children: ReactNode;
  className?: string;
  render?: RenderFunction<E>;
  variant?: ItemCardVariant;
};

export type ItemCardIconProps = ComponentPropsWithRef<"div"> & { children: ReactNode };
export type ItemCardContentProps = ComponentPropsWithRef<"div"> & { children: ReactNode };
export type ItemCardTitleProps = ComponentPropsWithRef<"span"> & { children: ReactNode };
export type ItemCardDescriptionProps = ComponentPropsWithRef<"span"> & { children: ReactNode };
export type ItemCardActionProps = ComponentPropsWithRef<"div"> & { children: ReactNode };

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export function ItemCardRoot<E extends keyof JSX.IntrinsicElements = "div">({
  children,
  className,
  render,
  variant = "default",
  ...props
}: ItemCardRootProps<E> & Omit<JSX.IntrinsicElements[E], keyof ItemCardRootProps<E>>) {
  const rootProps = {
    ...props,
    className: mergeClassNames("item-card", `item-card--${variant}`, className),
    "data-slot": "item-card",
    "data-variant": variant,
  } as unknown as JSX.IntrinsicElements[E];

  if (render) return render(rootProps);
  return createElement("div", rootProps as unknown as JSX.IntrinsicElements["div"], children);
}

function itemCardSlot<Tag extends "div" | "span">(tag: Tag, slot: string, displayName: string) {
  const Component = forwardRef<HTMLElement, ComponentPropsWithRef<Tag> & { children: ReactNode }>(
    function ItemCardSlot({ className, ...props }, ref) {
      return createElement(tag, {
        ...props,
        ref,
        className: mergeClassNames(`item-card__${slot}`, className),
        "data-slot": `item-card-${slot}`,
      });
    },
  );
  Component.displayName = displayName;
  return Component;
}

export const ItemCardIcon = itemCardSlot("div", "icon", "ItemCardIcon");
export const ItemCardContent = itemCardSlot("div", "content", "ItemCardContent");
export const ItemCardTitle = itemCardSlot("span", "title", "ItemCardTitle");
export const ItemCardDescription = itemCardSlot("span", "description", "ItemCardDescription");
export const ItemCardAction = itemCardSlot("div", "action", "ItemCardAction");

export const ItemCard = Object.assign(ItemCardRoot, {
  Root: ItemCardRoot,
  Icon: ItemCardIcon,
  Content: ItemCardContent,
  Title: ItemCardTitle,
  Description: ItemCardDescription,
  Action: ItemCardAction,
});
