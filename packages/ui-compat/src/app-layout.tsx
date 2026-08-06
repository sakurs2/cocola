"use client";

import { Button, Tooltip } from "@heroui/react";
import {
  createContext,
  forwardRef,
  useContext,
  useState,
  type ComponentPropsWithRef,
  type CSSProperties,
  type ReactNode,
} from "react";
import { Sidebar, useSidebar } from "./sidebar";

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

export type AppLayoutScrollMode = "content" | "page";

export type AppLayoutContextValue = {
  isAsideOpen: boolean;
  hasMobileAside: boolean;
  navigate?: (href: string) => void;
  setAsideOpen: (open: boolean) => void;
  toggleAside: () => void;
};

export const AppLayoutContext = createContext<AppLayoutContextValue | null>(null);

export function useAppLayout() {
  return useContext(AppLayoutContext);
}

export interface AppLayoutRootProps extends Omit<ComponentPropsWithRef<"div">, "children"> {
  aside?: ReactNode;
  asideMobile?: "hidden" | "sheet";
  asideDefaultSize?: number | string;
  asideMaxSize?: number | string;
  asideMinSize?: number | string;
  asideOpen?: boolean;
  asideResizable?: boolean;
  asideToggleShortcut?: string | false | null;
  children: ReactNode;
  defaultAsideOpen?: boolean;
  defaultSidebarOpen?: boolean;
  footer?: ReactNode;
  navbar?: ReactNode;
  navigate?: (href: string) => void;
  onAsideOpenChange?: (open: boolean) => void;
  onSidebarOpenChange?: (open: boolean) => void;
  reduceMotion?: boolean;
  scrollMode?: AppLayoutScrollMode;
  sidebar?: ReactNode;
  sidebarCollapsible?: "icon" | "none" | "offcanvas";
  sidebarDefaultSize?: number | string;
  sidebarMaxSize?: number | string;
  sidebarMinSize?: number | string;
  sidebarOpen?: boolean;
  sidebarResizable?: boolean;
  sidebarSide?: "left" | "right";
  sidebarVariant?: "floating" | "inset" | "sidebar";
  toggleShortcut?: string | false | null;
  toolbar?: ReactNode;
}

export const AppLayoutRoot = forwardRef<HTMLDivElement, AppLayoutRootProps>(function AppLayoutRoot(
  {
    aside,
    asideDefaultSize = "20rem",
    asideMaxSize: _asideMaxSize,
    asideMinSize: _asideMinSize,
    asideMobile: _asideMobile,
    asideOpen: asideOpenProp,
    asideResizable: _asideResizable,
    asideToggleShortcut: _asideToggleShortcut,
    children,
    className,
    defaultAsideOpen = true,
    defaultSidebarOpen = true,
    footer,
    navbar,
    navigate,
    onAsideOpenChange,
    onSidebarOpenChange,
    reduceMotion = false,
    scrollMode = "page",
    sidebar,
    sidebarCollapsible = "icon",
    sidebarDefaultSize,
    sidebarMaxSize: _sidebarMaxSize,
    sidebarMinSize: _sidebarMinSize,
    sidebarOpen,
    sidebarResizable: _sidebarResizable,
    sidebarSide = "left",
    sidebarVariant = "sidebar",
    style,
    toggleShortcut = "mod+b",
    toolbar,
    ...props
  },
  ref,
) {
  const [uncontrolledAsideOpen, setUncontrolledAsideOpen] = useState(defaultAsideOpen);
  const isAsideOpen = asideOpenProp ?? uncontrolledAsideOpen;
  const setAsideOpen = (open: boolean) => {
    if (asideOpenProp === undefined) setUncontrolledAsideOpen(open);
    onAsideOpenChange?.(open);
  };
  const sidebarWidth =
    typeof sidebarDefaultSize === "number" ? `${sidebarDefaultSize}%` : sidebarDefaultSize;
  const asideWidth =
    typeof asideDefaultSize === "number" ? `${asideDefaultSize}%` : asideDefaultSize;
  const layoutStyle = {
    ...style,
    ...(sidebarWidth ? { "--sidebar-default-size": sidebarWidth } : {}),
    ...(asideWidth ? { "--aside-default-size": asideWidth } : {}),
  } as CSSProperties;

  return (
    <AppLayoutContext.Provider
      value={{
        hasMobileAside: false,
        isAsideOpen,
        navigate,
        setAsideOpen,
        toggleAside: () => setAsideOpen(!isAsideOpen),
      }}
    >
      <Sidebar.Provider
        {...props}
        ref={ref}
        className={className}
        collapsible={sidebarCollapsible}
        defaultOpen={defaultSidebarOpen}
        navigate={navigate}
        open={sidebarOpen}
        reduceMotion={reduceMotion}
        side={sidebarSide}
        style={layoutStyle}
        toggleShortcut={toggleShortcut}
        variant={sidebarVariant}
        data-app-layout=""
        data-scroll-mode={scrollMode}
        onOpenChange={onSidebarOpenChange}
      >
        {sidebar}
        <div className="app-layout__body" data-slot="app-layout-body">
          {navbar ? (
            <header className="app-layout__header" data-slot="app-layout-header">
              {navbar}
            </header>
          ) : null}
          {toolbar ? (
            <div className="app-layout__toolbar" data-slot="app-layout-toolbar">
              {toolbar}
            </div>
          ) : null}
          <main
            aria-label={scrollMode === "content" ? "Scrollable main content" : undefined}
            className="app-layout__main"
            data-slot="app-layout-main"
            tabIndex={scrollMode === "content" ? 0 : undefined}
          >
            {children}
          </main>
          {footer ? (
            <footer className="app-layout__footer" data-slot="app-layout-footer">
              {footer}
            </footer>
          ) : null}
        </div>
        {aside && isAsideOpen ? (
          <aside
            className="app-layout__aside"
            data-slot="app-layout-aside"
            style={{ width: "var(--aside-default-size, 20rem)" }}
          >
            {aside}
          </aside>
        ) : null}
      </Sidebar.Provider>
    </AppLayoutContext.Provider>
  );
});

export interface AppLayoutMenuToggleProps extends ComponentPropsWithRef<typeof Button> {
  children?: ReactNode;
  tooltip?: ReactNode;
  tooltipProps?: { delay?: number; closeDelay?: number };
}

export function AppLayoutMenuToggle({
  children,
  className,
  tooltip,
  tooltipProps,
  ...props
}: AppLayoutMenuToggleProps) {
  const { toggleSidebar } = useSidebar();
  const button = (
    <Button
      {...props}
      isIconOnly
      className={mergeClassNames("app-layout__menu-toggle", className as string | undefined)}
      data-slot="app-layout-menu-toggle"
      size="sm"
      variant="ghost"
      onPress={toggleSidebar}
    >
      {children ?? <span aria-hidden="true">☰</span>}
    </Button>
  );
  if (!tooltip) return button;
  return (
    <Tooltip delay={tooltipProps?.delay} closeDelay={tooltipProps?.closeDelay}>
      {button}
      <Tooltip.Content>{tooltip}</Tooltip.Content>
    </Tooltip>
  );
}

export interface AppLayoutAsideTriggerProps extends ComponentPropsWithRef<typeof Button> {
  children?: ReactNode;
  closedTooltip?: ReactNode;
  openTooltip?: ReactNode;
}

export function AppLayoutAsideTrigger({
  children,
  className,
  closedTooltip,
  openTooltip,
  ...props
}: AppLayoutAsideTriggerProps) {
  const context = useAppLayout();
  if (!context) throw new Error("AppLayout.AsideTrigger must be used inside AppLayout.");
  const button = (
    <Button
      {...props}
      isIconOnly
      className={mergeClassNames("app-layout__aside-trigger", className as string | undefined)}
      size="sm"
      variant="ghost"
      onPress={context.toggleAside}
    >
      {children ?? <span aria-hidden="true">▥</span>}
    </Button>
  );
  const content = context.isAsideOpen ? openTooltip : closedTooltip;
  return content ? (
    <Tooltip>
      {button}
      <Tooltip.Content>{content}</Tooltip.Content>
    </Tooltip>
  ) : (
    button
  );
}

export function AppLayoutMobileAside({ children: _children }: { children: ReactNode }) {
  return null;
}

export const AppLayout = Object.assign(AppLayoutRoot, {
  AsideTrigger: AppLayoutAsideTrigger,
  MenuToggle: AppLayoutMenuToggle,
  MobileAside: AppLayoutMobileAside,
  Root: AppLayoutRoot,
});
