"use client";

import { Button, Separator, Tooltip } from "@heroui/react";
import {
  createContext,
  forwardRef,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentProps,
  type ComponentPropsWithRef,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { Sheet } from "./sheet";

type SidebarNavigate = (href: string) => void;

type SidebarContextValue = {
  collapsible: "icon" | "none" | "offcanvas";
  isMobile: boolean;
  isMobileOpen: boolean;
  isOpen: boolean;
  navigate?: SidebarNavigate;
  reduceMotion: boolean;
  setMobileOpen: (open: boolean) => void;
  setOpen: (open: boolean) => void;
  side: "left" | "right";
  toggleSidebar: () => void;
  variant: "floating" | "inset" | "sidebar";
};

const SidebarContext = createContext<SidebarContextValue | null>(null);

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export function useSidebar() {
  const value = useContext(SidebarContext);
  if (!value) throw new Error("Sidebar compound components must be used inside Sidebar.Provider.");
  return value;
}

export interface SidebarProviderProps extends ComponentPropsWithRef<"div"> {
  children: ReactNode;
  collapsible?: "icon" | "none" | "offcanvas";
  defaultOpen?: boolean;
  navigate?: SidebarNavigate;
  onOpenChange?: (open: boolean) => void;
  open?: boolean;
  reduceMotion?: boolean;
  side?: "left" | "right";
  toggleShortcut?: string | false | null;
  variant?: "floating" | "inset" | "sidebar";
}

export const SidebarProvider = forwardRef<HTMLDivElement, SidebarProviderProps>(
  function SidebarProvider(
    {
      children,
      className,
      collapsible = "icon",
      defaultOpen = true,
      navigate,
      onOpenChange,
      open: openProp,
      reduceMotion = false,
      side = "left",
      toggleShortcut = "mod+b",
      variant = "sidebar",
      ...props
    },
    ref,
  ) {
    const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen);
    const [isMobileOpen, setMobileOpen] = useState(false);
    const [isMobile, setIsMobile] = useState(false);
    const isOpen = openProp ?? uncontrolledOpen;

    const setOpen = (open: boolean) => {
      if (openProp === undefined) setUncontrolledOpen(open);
      onOpenChange?.(open);
    };

    const toggleSidebar = () => {
      if (typeof window !== "undefined" && window.matchMedia("(max-width: 64rem)").matches) {
        setMobileOpen((open) => !open);
        return;
      }
      setOpen(!isOpen);
    };

    useEffect(() => {
      const query = window.matchMedia("(max-width: 64rem)");
      const update = () => setIsMobile(query.matches);
      update();
      query.addEventListener("change", update);
      return () => query.removeEventListener("change", update);
    }, []);

    useEffect(() => {
      if (!toggleShortcut) return;
      const parts = toggleShortcut.toLowerCase().split("+");
      const key = parts.at(-1);
      if (!key) return;

      const onKeyDown = (event: KeyboardEvent) => {
        const modMatches =
          !parts.includes("mod") ||
          (navigator.platform.includes("Mac") ? event.metaKey : event.ctrlKey);
        const metaMatches = (!parts.includes("meta") && !parts.includes("cmd")) || event.metaKey;
        const ctrlMatches = !parts.includes("ctrl") || event.ctrlKey;
        const shiftMatches = !parts.includes("shift") || event.shiftKey;
        const altMatches = !parts.includes("alt") || event.altKey;
        if (
          event.key.toLowerCase() === key &&
          modMatches &&
          metaMatches &&
          ctrlMatches &&
          shiftMatches &&
          altMatches
        ) {
          event.preventDefault();
          toggleSidebar();
        }
      };
      window.addEventListener("keydown", onKeyDown);
      return () => window.removeEventListener("keydown", onKeyDown);
    }, [isOpen, toggleShortcut]);

    const value = useMemo<SidebarContextValue>(
      () => ({
        collapsible,
        isMobile,
        isMobileOpen,
        isOpen,
        navigate,
        reduceMotion,
        setMobileOpen,
        setOpen,
        side,
        toggleSidebar,
        variant,
      }),
      [collapsible, isMobile, isMobileOpen, isOpen, navigate, reduceMotion, side, variant],
    );

    return (
      <SidebarContext.Provider value={value}>
        <div
          {...props}
          ref={ref}
          className={mergeClassNames("sidebar__provider", className)}
          data-sidebar="provider"
          data-slot="sidebar-provider"
          data-state={isOpen ? "expanded" : "collapsed"}
        >
          {children}
        </div>
      </SidebarContext.Provider>
    );
  },
);

export interface SidebarRootProps extends ComponentPropsWithRef<"aside"> {
  children: ReactNode;
}

export const SidebarRoot = forwardRef<HTMLElement, SidebarRootProps>(function SidebarRoot(
  { children, className, ...props },
  ref,
) {
  const { collapsible, isOpen, side, variant } = useSidebar();
  return (
    <div
      className="sidebar__offcanvas-wrapper"
      data-side={side}
      data-state={isOpen ? "expanded" : "collapsed"}
    >
      <aside
        {...props}
        ref={ref}
        className={mergeClassNames(
          "sidebar",
          `sidebar--${side}`,
          `sidebar--${variant === "sidebar" ? "default" : variant}`,
          className,
        )}
        data-collapsible={collapsible}
        data-side={side}
        data-slot="sidebar"
        data-state={isOpen ? "expanded" : "collapsed"}
        data-variant={variant}
      >
        {children}
      </aside>
    </div>
  );
});

type DivSlotProps = ComponentPropsWithRef<"div"> & { children: ReactNode };
type SpanSlotProps = ComponentPropsWithRef<"span"> & { children: ReactNode };

function divSlot(displayName: string, slot: string) {
  const Component = forwardRef<HTMLDivElement, DivSlotProps>(function DivSlot(
    { children, className, ...props },
    ref,
  ) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames(displayName, className)}
        data-slot={slot}
      >
        {children}
      </div>
    );
  });
  return Component;
}

export const SidebarHeader = divSlot("sidebar__header", "sidebar-header");
export const SidebarFooter = divSlot("sidebar__footer", "sidebar-footer");
export const SidebarGroupLabel = divSlot("sidebar__group-label", "sidebar-group-label");
export const SidebarMenuActions = divSlot("sidebar__menu-actions", "sidebar-menu-actions");

export interface SidebarContentProps extends DivSlotProps {}

export const SidebarContent = forwardRef<HTMLDivElement, SidebarContentProps>(
  function SidebarContent({ children, className, ...props }, ref) {
    return (
      <div
        {...props}
        ref={ref}
        className={mergeClassNames(
          "scroll-shadow scroll-shadow--hide-scrollbar scroll-shadow--vertical scroll-shadow--fade sidebar__content",
          className,
        )}
        data-orientation="vertical"
        data-slot="sidebar-content"
      >
        {children}
      </div>
    );
  },
);

export interface SidebarGroupProps extends DivSlotProps {
  closeMobileOnAction?: boolean;
}

export const SidebarGroup = forwardRef<HTMLDivElement, SidebarGroupProps>(function SidebarGroup(
  { children, className, closeMobileOnAction: _closeMobileOnAction, ...props },
  ref,
) {
  return (
    <div
      {...props}
      ref={ref}
      className={mergeClassNames("sidebar__group", className)}
      data-slot="sidebar-group"
    >
      {children}
    </div>
  );
});

export interface SidebarMenuProps extends DivSlotProps {
  closeMobileOnAction?: boolean;
  reduceMotion?: boolean;
  showGuideLines?: boolean | "hover";
}

export const SidebarMenu = forwardRef<HTMLDivElement, SidebarMenuProps>(function SidebarMenu(
  {
    children,
    className,
    closeMobileOnAction: _closeMobileOnAction,
    reduceMotion: _reduceMotion,
    showGuideLines = true,
    onKeyDown,
    ...props
  },
  ref,
) {
  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    onKeyDown?.(event);
    if (event.defaultPrevented || !["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      return;
    }
    const items = Array.from(
      event.currentTarget.querySelectorAll<HTMLElement>(
        ":scope > [role='row']:not([aria-disabled='true'])",
      ),
    );
    if (!items.length) return;
    const activeIndex = items.indexOf(document.activeElement as HTMLElement);
    const nextIndex =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? items.length - 1
          : event.key === "ArrowDown"
            ? Math.min(activeIndex + 1, items.length - 1)
            : Math.max(activeIndex < 0 ? items.length - 1 : activeIndex - 1, 0);
    event.preventDefault();
    items[nextIndex]?.focus();
  };

  return (
    <div
      {...props}
      ref={ref}
      className={mergeClassNames("sidebar__menu", className)}
      data-guide-lines={showGuideLines === "hover" ? "hover" : showGuideLines ? "always" : "never"}
      data-sidebar="menu"
      data-slot="sidebar-menu"
      role="treegrid"
      tabIndex={0}
      onKeyDown={handleKeyDown}
    >
      {children}
    </div>
  );
});

export interface SidebarMenuItemProps extends Omit<ComponentPropsWithRef<"div">, "onAction"> {
  children: ReactNode;
  closeMobileOnAction?: boolean;
  forceReload?: boolean;
  href?: string;
  isCurrent?: boolean;
  onAction?: () => void;
  rel?: string;
  target?: string;
  textValue?: string;
  tooltip?: ReactNode;
  tooltipProps?: { content?: ReactNode };
}

export const SidebarMenuItem = forwardRef<HTMLDivElement, SidebarMenuItemProps>(
  function SidebarMenuItem(
    {
      children,
      className,
      closeMobileOnAction = true,
      forceReload,
      href,
      isCurrent,
      onAction,
      onClick,
      onKeyDown,
      onPointerDown,
      onPointerLeave,
      onPointerUp,
      rel: _rel,
      target,
      textValue,
      tooltip: _tooltip,
      tooltipProps: _tooltipProps,
      ...props
    },
    ref,
  ) {
    const { isMobile, navigate, setMobileOpen } = useSidebar();
    const [pressed, setPressed] = useState(false);

    const activate = (event?: ReactMouseEvent<HTMLDivElement>) => {
      const targetElement = event?.target as HTMLElement | undefined;
      const interactive = targetElement?.closest(
        "button, a, input, textarea, select, [role='button'], [role='menuitem']",
      );
      if (interactive && interactive !== event?.currentTarget) return;
      onAction?.();
      if (href) {
        const modified = Boolean(
          event && (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey),
        );
        if (modified || target === "_blank") {
          window.open(href, target ?? "_blank", "noopener,noreferrer");
        } else if (forceReload || !navigate) {
          window.location.assign(href);
        } else {
          navigate(href);
        }
      }
      if (isMobile && closeMobileOnAction) setMobileOpen(false);
    };

    return (
      <div
        {...props}
        ref={ref}
        aria-label={props["aria-label"] ?? textValue}
        className={mergeClassNames("sidebar__menu-item", className)}
        data-current={isCurrent ? "true" : undefined}
        data-pressed={pressed ? "true" : undefined}
        data-slot="sidebar-menu-item"
        role="row"
        tabIndex={-1}
        onClick={(event) => {
          onClick?.(event);
          if (!event.defaultPrevented) activate(event);
        }}
        onKeyDown={(event) => {
          onKeyDown?.(event);
          if (!event.defaultPrevented && (event.key === "Enter" || event.key === " ")) {
            event.preventDefault();
            activate();
          }
        }}
        onPointerDown={(event) => {
          setPressed(true);
          onPointerDown?.(event);
        }}
        onPointerLeave={(event) => {
          setPressed(false);
          onPointerLeave?.(event);
        }}
        onPointerUp={(event) => {
          setPressed(false);
          onPointerUp?.(event);
        }}
      >
        <div aria-colindex={1} role="gridcell" style={{ display: "contents" }}>
          <div className="sidebar__menu-item-content" data-slot="sidebar-menu-item-content">
            {children}
          </div>
        </div>
      </div>
    );
  },
);

export const SidebarMenuIcon = forwardRef<HTMLSpanElement, SpanSlotProps>(function SidebarMenuIcon(
  { children, className, ...props },
  ref,
) {
  return (
    <span
      {...props}
      ref={ref}
      className={mergeClassNames("sidebar__menu-icon", className)}
      data-slot="sidebar-menu-icon"
    >
      {children}
    </span>
  );
});

export const SidebarMenuLabel = forwardRef<HTMLSpanElement, SpanSlotProps>(
  function SidebarMenuLabel({ children, className, ...props }, ref) {
    return (
      <span
        {...props}
        ref={ref}
        className={mergeClassNames("sidebar__menu-label", className)}
        data-sidebar="label"
        data-slot="sidebar-menu-label"
      >
        <span className="sidebar__menu-label-text" data-slot="sidebar-menu-label-text">
          {children}
        </span>
      </span>
    );
  },
);

export interface SidebarMobileProps extends DivSlotProps {
  backdrop?: "blur" | "opaque" | "transparent";
}

export function SidebarMobile({
  backdrop = "blur",
  children,
  className,
  ...props
}: SidebarMobileProps) {
  const { isMobileOpen, setMobileOpen, side } = useSidebar();
  return (
    <Sheet isOpen={isMobileOpen} placement={side} onOpenChange={setMobileOpen}>
      <Sheet.Backdrop variant={backdrop}>
        <Sheet.Content className={mergeClassNames("sidebar__mobile-sheet", className)}>
          <Sheet.Dialog className="sidebar__mobile-dialog">
            <div {...props} className="sidebar__mobile" data-slot="sidebar-mobile">
              {children}
            </div>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
  );
}

export interface SidebarTriggerProps extends ComponentPropsWithRef<typeof Button> {
  children?: ReactNode;
}

export function SidebarTrigger({ children, className, ...props }: SidebarTriggerProps) {
  const { toggleSidebar } = useSidebar();
  return (
    <Button
      {...props}
      isIconOnly
      className={mergeClassNames("sidebar__trigger", className as string | undefined)}
      size="sm"
      variant="ghost"
      onPress={toggleSidebar}
    >
      {children ?? <span aria-hidden="true">☰</span>}
    </Button>
  );
}

export const SidebarSeparator = forwardRef<HTMLElement, ComponentPropsWithRef<typeof Separator>>(
  function SidebarSeparator({ className, ...props }, ref) {
    return (
      <Separator
        {...props}
        ref={ref}
        className={mergeClassNames("sidebar__separator", className as string | undefined)}
      />
    );
  },
);

export interface SidebarTooltipProps extends Omit<ComponentProps<typeof Tooltip>, "children"> {
  children: ReactNode;
  content: ReactNode;
}

export function SidebarTooltip({ children, content, ...props }: SidebarTooltipProps) {
  return (
    <Tooltip {...props}>
      {children}
      <Tooltip.Content>{content}</Tooltip.Content>
    </Tooltip>
  );
}

export const Sidebar = Object.assign(SidebarRoot, {
  Content: SidebarContent,
  Footer: SidebarFooter,
  Group: SidebarGroup,
  GroupLabel: SidebarGroupLabel,
  Header: SidebarHeader,
  Menu: SidebarMenu,
  MenuActions: SidebarMenuActions,
  MenuIcon: SidebarMenuIcon,
  MenuItem: SidebarMenuItem,
  MenuLabel: SidebarMenuLabel,
  Mobile: SidebarMobile,
  Provider: SidebarProvider,
  Root: SidebarRoot,
  Separator: SidebarSeparator,
  Tooltip: SidebarTooltip,
  Trigger: SidebarTrigger,
});
