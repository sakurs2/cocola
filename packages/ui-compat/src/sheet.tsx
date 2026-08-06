"use client";

import { Drawer } from "@heroui/react";
import {
  cloneElement,
  createContext,
  forwardRef,
  useContext,
  useState,
  type ComponentProps,
  type ComponentPropsWithRef,
  type ReactElement,
  type ReactNode,
} from "react";

export type SheetPlacement = "top" | "bottom" | "left" | "right";

export type SheetRootProps = {
  children?: ReactNode;
  isOpen?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  onClose?: () => void;
  isDismissable?: boolean;
  placement?: SheetPlacement;
  shouldAutoFocus?: boolean;
  container?: HTMLElement | null;
  isDetached?: boolean;
  activeSnapPoint?: number | string | null;
  onActiveSnapPointChange?: (snapPoint: number | string | null) => void;
  snapPoints?: Array<number | string>;
  fadeFromIndex?: number;
  closeThreshold?: number;
  noBodyStyles?: boolean;
  shouldScaleBackground?: boolean;
  setBackgroundColorOnScale?: boolean;
  scrollLockTimeout?: number;
  isFixed?: boolean;
  isHandleOnly?: boolean;
  isModal?: boolean;
  isNested?: boolean;
  disablePreventScroll?: boolean;
  repositionInputs?: boolean;
  snapToSequentialPoint?: boolean;
  onAnimationEnd?: (open: boolean) => void;
  preventScrollRestoration?: boolean;
};

export type SheetTriggerProps = {
  children: ReactElement<{ onPress?: () => void }>;
};
export type SheetCloseProps = SheetTriggerProps;
export type SheetBackdropProps = Omit<ComponentProps<typeof Drawer.Backdrop>, "variant"> & {
  variant?: "blur" | "opaque" | "transparent";
};
export type SheetContentProps = Omit<ComponentProps<typeof Drawer.Content>, "placement">;
export type SheetDialogProps = ComponentProps<typeof Drawer.Dialog>;
export type SheetHeaderProps = ComponentProps<typeof Drawer.Header>;
export type SheetHeadingProps = ComponentProps<typeof Drawer.Heading>;
export type SheetBodyProps = ComponentProps<typeof Drawer.Body>;
export type SheetFooterProps = ComponentProps<typeof Drawer.Footer>;
export type SheetHandleProps = ComponentProps<typeof Drawer.Handle> & {
  preventCycle?: boolean;
};
export type SheetCloseTriggerProps = ComponentProps<typeof Drawer.CloseTrigger>;

type SheetContextValue = {
  isDismissable: boolean;
  placement: SheetPlacement;
  setOpen: (open: boolean) => void;
};

const SheetContext = createContext<SheetContextValue | null>(null);

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

function useSheetContext() {
  const value = useContext(SheetContext);
  if (!value) throw new Error("Sheet compound components must be used inside Sheet.");
  return value;
}

export function SheetRoot({
  children,
  defaultOpen,
  isDismissable = true,
  isOpen,
  onAnimationEnd,
  onClose,
  onOpenChange,
  placement = "bottom",
}: SheetRootProps) {
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen ?? false);
  const open = isOpen ?? uncontrolledOpen;
  const setOpen = (open: boolean) => {
    if (isOpen === undefined) setUncontrolledOpen(open);
    onOpenChange?.(open);
    if (!open) onClose?.();
    onAnimationEnd?.(open);
  };

  return (
    <SheetContext.Provider value={{ isDismissable, placement, setOpen }}>
      <Drawer.Root isOpen={open} onOpenChange={setOpen}>
        {children}
      </Drawer.Root>
    </SheetContext.Provider>
  );
}

export function SheetNestedRoot(props: SheetRootProps) {
  return <SheetRoot {...props} isNested />;
}

export function SheetTrigger({ children }: SheetTriggerProps) {
  const { setOpen } = useSheetContext();
  return cloneElement(children, {
    onPress: () => {
      children.props.onPress?.();
      setOpen(true);
    },
  });
}

export function SheetClose({ children }: SheetCloseProps) {
  const { setOpen } = useSheetContext();
  return cloneElement(children, {
    onPress: () => {
      children.props.onPress?.();
      setOpen(false);
    },
  });
}

export const SheetBackdrop = forwardRef<HTMLDivElement, SheetBackdropProps>(function SheetBackdrop(
  { className, variant = "opaque", ...props },
  ref,
) {
  const { isDismissable } = useSheetContext();
  return (
    <Drawer.Backdrop
      {...props}
      ref={ref}
      className={mergeClassNames(
        "sheet__backdrop",
        `sheet__backdrop--${variant}`,
        className as string | undefined,
      )}
      data-slot="sheet-backdrop"
      isDismissable={isDismissable}
      variant={variant}
    />
  );
});

export const SheetContent = forwardRef<HTMLDivElement, SheetContentProps>(function SheetContent(
  { className, ...props },
  ref,
) {
  const { placement } = useSheetContext();
  const contentProps = {
    ...props,
    ref,
    className: mergeClassNames(
      "sheet__content",
      `sheet__content--${placement}`,
      className as string | undefined,
    ),
    "data-slot": "sheet-content",
    placement,
  } as unknown as ComponentProps<typeof Drawer.Content>;
  return <Drawer.Content {...contentProps} />;
});

export function SheetDialog({ className, ...props }: SheetDialogProps) {
  const { placement } = useSheetContext();
  return (
    <Drawer.Dialog
      {...props}
      className={mergeClassNames(
        "sheet__dialog",
        `sheet__dialog--${placement}`,
        className as string | undefined,
      )}
      data-slot="sheet-dialog"
    />
  );
}

export function SheetHeader({ className, ...props }: SheetHeaderProps) {
  return (
    <Drawer.Header
      {...props}
      className={mergeClassNames("sheet__header", className as string | undefined)}
      data-slot="sheet-header"
    />
  );
}

export function SheetHeading({ className, ...props }: SheetHeadingProps) {
  return (
    <Drawer.Heading
      {...props}
      className={mergeClassNames("sheet__heading", className as string | undefined)}
      data-slot="sheet-heading"
    />
  );
}

export function SheetBody({ className, ...props }: SheetBodyProps) {
  return (
    <Drawer.Body
      {...props}
      className={mergeClassNames("sheet__body", className as string | undefined)}
      data-slot="sheet-body"
    />
  );
}

export function SheetFooter({ className, ...props }: SheetFooterProps) {
  return (
    <Drawer.Footer
      {...props}
      className={mergeClassNames("sheet__footer", className as string | undefined)}
      data-slot="sheet-footer"
    />
  );
}

export function SheetHandle({
  className,
  preventCycle: _preventCycle,
  ...props
}: SheetHandleProps) {
  return (
    <Drawer.Handle
      {...props}
      className={mergeClassNames("sheet__handle", className as string | undefined)}
      data-slot="sheet-handle"
    />
  );
}

export function SheetCloseTrigger({ className, ...props }: SheetCloseTriggerProps) {
  return (
    <Drawer.CloseTrigger
      {...props}
      className={mergeClassNames("sheet__close-trigger", className as string | undefined)}
      data-slot="sheet-close-trigger"
    />
  );
}

export const Sheet = Object.assign(SheetRoot, {
  Root: SheetRoot,
  NestedRoot: SheetNestedRoot,
  Trigger: SheetTrigger,
  Close: SheetClose,
  Backdrop: SheetBackdrop,
  Content: SheetContent,
  Dialog: SheetDialog,
  Header: SheetHeader,
  Heading: SheetHeading,
  Body: SheetBody,
  Footer: SheetFooter,
  Handle: SheetHandle,
  CloseTrigger: SheetCloseTrigger,
});
