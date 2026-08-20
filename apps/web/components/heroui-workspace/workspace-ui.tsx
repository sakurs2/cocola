"use client";

import { ArrowRight, Search } from "lucide-react";
import { Button, Card, SearchField } from "@heroui/react";
import { Sheet } from "@cocola/ui-compat/sheet";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import type { ReactNode } from "react";

export function WorkspacePageFrame({ children }: { children: ReactNode }) {
  return (
    <div className="cocola-web-page mx-auto flex w-full max-w-7xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
      {children}
    </div>
  );
}

export function WorkspacePageHeader({
  action,
  description,
  icon,
  title,
}: {
  action?: ReactNode;
  description: string;
  icon: ReactNode;
  title: string;
}) {
  return (
    <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex items-center gap-3">
        <span className="bg-accent-soft text-accent flex size-11 shrink-0 items-center justify-center rounded-2xl">
          {icon}
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-[-0.03em]">{title}</h1>
          <p className="text-muted mt-1 max-w-2xl text-sm leading-5">{description}</p>
        </div>
      </div>
      {action ? <div className="cocola-web-page-action shrink-0">{action}</div> : null}
    </header>
  );
}

export function WorkspacePageAction({
  children,
  href,
  isDisabled,
  onPress,
}: {
  children: ReactNode;
  href?: string;
  isDisabled?: boolean;
  onPress?: () => void;
}) {
  const router = useRouter();

  if (href) {
    return (
      <Button
        className="cocola-web-page-primary-action"
        isDisabled={isDisabled}
        variant="primary"
        onPress={() => router.push(href)}
      >
        {children}
      </Button>
    );
  }
  return (
    <Button
      className="cocola-web-page-primary-action"
      isDisabled={isDisabled}
      variant="primary"
      onPress={onPress}
    >
      {children}
    </Button>
  );
}

export function WorkspaceSearch({
  placeholder,
  value,
  onChange,
}: {
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <SearchField
      aria-label={placeholder}
      className="w-full sm:w-[320px]"
      value={value}
      onChange={onChange}
    >
      <SearchField.Group>
        <SearchField.SearchIcon>
          <Search className="size-4" />
        </SearchField.SearchIcon>
        <SearchField.Input placeholder={placeholder} />
        <SearchField.ClearButton />
      </SearchField.Group>
    </SearchField>
  );
}

export function WorkspaceSectionHeader({
  description,
  title,
}: {
  description: string;
  title: string;
}) {
  return (
    <div>
      <h2 className="text-sm font-semibold">{title}</h2>
      <p className="text-muted mt-1 text-xs">{description}</p>
    </div>
  );
}

export function WorkspaceCatalogGrid({ children }: { children: ReactNode }) {
  return (
    <section className="cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-3">
      {children}
    </section>
  );
}

export function WorkspaceCatalogCard({
  description,
  footerLabel,
  footerMeta,
  href,
  icon,
  iconClassName,
  metadata,
  status,
  title,
}: {
  description: string;
  footerLabel: string;
  footerMeta: ReactNode;
  href: string;
  icon: ReactNode;
  iconClassName: string;
  metadata?: ReactNode;
  status?: ReactNode;
  title: string;
}) {
  return (
    <Link
      className="cocola-web-catalog-trigger group rounded-2xl no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus)]"
      href={href}
    >
      <Card className="cocola-web-catalog-card h-full min-h-[14.25rem] p-5">
        <Card.Content className="flex h-full min-w-0 flex-col items-start gap-3 p-0">
          <span className="flex w-full items-start justify-between gap-3">
            <span
              className={`cocola-web-catalog-card-icon flex size-10 shrink-0 items-center justify-center rounded-2xl ${iconClassName}`}
            >
              {icon}
            </span>
            {status}
          </span>
          <span className="text-foreground font-semibold">{title}</span>
          <span className="text-muted line-clamp-2 min-h-10 text-sm leading-5">{description}</span>
          {metadata ? (
            <span className="flex min-w-0 flex-wrap items-center gap-2">{metadata}</span>
          ) : null}
          <span className="text-muted mt-auto flex w-full min-w-0 items-center justify-between gap-3 text-xs">
            <span className="min-w-0 truncate">{footerMeta}</span>
            <span className="text-accent flex shrink-0 items-center gap-1 font-medium">
              {footerLabel}
              <ArrowRight className="cocola-web-catalog-card-arrow size-3.5" />
            </span>
          </span>
        </Card.Content>
      </Card>
    </Link>
  );
}

export function WorkspaceEntitySheet({
  children,
  description,
  isOpen,
  title,
  onOpenChange,
}: {
  children: ReactNode;
  description?: string;
  isOpen: boolean;
  title?: string;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("common.dialog");
  return (
    <Sheet isOpen={isOpen} placement="right" onOpenChange={onOpenChange}>
      <Sheet.Backdrop>
        <Sheet.Content className="w-full md:w-[480px]">
          <Sheet.Dialog>
            <Sheet.CloseTrigger aria-label={t("closeDetails")} />
            <Sheet.Header>
              <Sheet.Heading>{title}</Sheet.Heading>
              {description ? <p className="text-muted text-sm leading-6">{description}</p> : null}
            </Sheet.Header>
            <Sheet.Body>{children}</Sheet.Body>
          </Sheet.Dialog>
        </Sheet.Content>
      </Sheet.Backdrop>
    </Sheet>
  );
}

export function WorkspaceDetailGrid({
  className = "",
  rows,
}: {
  className?: string;
  rows: readonly (readonly [string, ReactNode])[];
}) {
  return (
    <dl className={`grid gap-3 ${className}`}>
      {rows.map(([label, value]) => (
        <div
          key={label}
          className="bg-surface-secondary flex items-center justify-between gap-4 rounded-2xl px-4 py-3"
        >
          <dt className="text-muted text-xs">{label}</dt>
          <dd className="truncate text-right text-sm font-medium">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
