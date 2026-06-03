"use client";

import * as React from "react";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { StreamHealth, StreamStatus, streamStatus } from "@/lib/api";
import { useT } from "@/lib/i18n";

// Map the derived status to a badge variant. "stale" deserves a distinct, alarming
// look from a clean "down" so operators can tell a quiet-but-connected listener
// from a disconnected one.
const VARIANT: Record<
  StreamStatus,
  "success" | "warning" | "destructive" | "outline"
> = {
  healthy: "success",
  idle: "outline",
  stale: "warning",
  degraded: "warning",
  down: "destructive",
};

export function StreamStatusBadge({
  stream,
  staleAfterMs,
  nowMs,
}: {
  stream: StreamHealth;
  staleAfterMs?: number;
  nowMs?: number;
}) {
  const t = useT();
  const status = streamStatus(stream, nowMs, staleAfterMs);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={VARIANT[status]} className="cursor-default">
          {t(`stream.status.${status}`)}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{t(`stream.status.tip.${status}`)}</TooltipContent>
    </Tooltip>
  );
}

// Localized "N ago" for a unix-seconds timestamp. Returns the i18n "never" string
// when there is no timestamp.
export function useRelativeTime() {
  const t = useT();
  return React.useCallback(
    (unixSec: number, nowMs: number = Date.now()): string => {
      if (!unixSec) return t("common.never");
      const sec = Math.max(0, Math.round(nowMs / 1000 - unixSec));
      if (sec < 2) return t("common.justnow");
      let span: string;
      if (sec < 60) span = `${sec}s`;
      else if (sec < 3600) span = `${Math.floor(sec / 60)}m`;
      else if (sec < 86400) span = `${Math.floor(sec / 3600)}h`;
      else span = `${Math.floor(sec / 86400)}d`;
      return t("common.ago", { n: span });
    },
    [t],
  );
}
