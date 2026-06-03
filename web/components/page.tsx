import * as React from "react";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { useT } from "@/lib/i18n";

export function PageHeader({
  title,
  subtitle,
  actions,
  className,
}: {
  title: string;
  subtitle?: string;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "mb-6 flex flex-col items-start justify-between gap-3 sm:flex-row sm:items-end",
        className,
      )}
    >
      <div className="space-y-1.5">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {subtitle && (
          <p className="text-muted-foreground text-sm">{subtitle}</p>
        )}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

export function StatCard({
  label,
  value,
  hint,
  tone,
  className,
}: {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  tone?: "default" | "success" | "warning" | "destructive" | "muted";
  className?: string;
}) {
  const toneClass =
    tone === "success"
      ? "text-success"
      : tone === "warning"
        ? "text-warning"
        : tone === "destructive"
          ? "text-destructive"
          : tone === "muted"
            ? "text-muted-foreground"
            : "";
  return (
    <Card className={cn("py-5", className)}>
      <CardContent className="space-y-1.5">
        <div className="text-muted-foreground text-xs font-medium uppercase tracking-wide">
          {label}
        </div>
        <div className={cn("text-2xl font-semibold tabular-nums", toneClass)}>
          {value}
        </div>
        {hint && (
          <div className="text-muted-foreground text-xs">{hint}</div>
        )}
      </CardContent>
    </Card>
  );
}

export function EmptyState({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "text-muted-foreground flex items-center justify-center px-6 py-10 text-sm",
        className,
      )}
    >
      {children}
    </div>
  );
}

// QueryState renders a consistent loading skeleton or an error row (with retry)
// for a TanStack Query result, falling back to its children once data is present.
// It disambiguates "loading" / "errored" / "genuinely empty", which the bare
// `data ?? []` pattern conflated everywhere.
export function QueryState({
  isLoading,
  isError,
  error,
  onRetry,
  rows = 3,
  children,
}: {
  isLoading: boolean;
  isError: boolean;
  error?: unknown;
  onRetry?: () => void;
  rows?: number;
  children?: React.ReactNode;
}) {
  const t = useT();
  if (isError) {
    const msg = error instanceof Error ? error.message : t("common.error");
    return (
      <div className="text-destructive flex flex-col items-center justify-center gap-3 px-6 py-10 text-sm">
        <span className="font-medium">
          {t("common.error")}
          {msg ? ` — ${msg}` : ""}
        </span>
        {onRetry && (
          <Button size="sm" variant="outline" onClick={onRetry}>
            {t("common.retry")}
          </Button>
        )}
      </div>
    );
  }
  if (isLoading) {
    return (
      <div className="space-y-2 px-6 py-4">
        {Array.from({ length: rows }).map((_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    );
  }
  return <>{children}</>;
}
