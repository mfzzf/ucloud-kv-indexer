"use client";

import { IconWorld, IconCircleFilled } from "@tabler/icons-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useCluster } from "@/lib/cluster";
import { useT } from "@/lib/i18n";

export function ClusterSwitcher() {
  const { cluster, setCluster, clusters, multiCluster } = useCluster();
  const t = useT();

  // Single-backend mode (no gateway): nothing to switch, hide entirely.
  if (!multiCluster) return null;

  const current =
    cluster === "all" ? t("cluster.all") : cluster;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="h-8 gap-1.5">
          <IconWorld className="size-4" />
          <span className="max-w-[10rem] truncate font-medium">{current}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[14rem]">
        <DropdownMenuLabel>{t("cluster.label")}</DropdownMenuLabel>
        <DropdownMenuItem onClick={() => setCluster("all")}>
          <span className="flex-1">{t("cluster.all")}</span>
          {cluster === "all" && <IconCircleFilled className="size-2.5 text-primary" />}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {clusters.map((c) => {
          const allHealthy = c.backends.every((b) => b.healthy);
          const anyHealthy = c.backends.some((b) => b.healthy);
          const tone = allHealthy
            ? "text-emerald-500"
            : anyHealthy
              ? "text-amber-500"
              : "text-destructive";
          return (
            <DropdownMenuItem
              key={c.cluster}
              onClick={() => setCluster(c.cluster)}
              className="gap-2"
            >
              <IconCircleFilled className={`size-2.5 ${tone}`} />
              <span className="flex-1">{c.cluster}</span>
              <span className="text-muted-foreground text-xs tabular-nums">
                {c.backends.filter((b) => b.healthy).length}/{c.backends.length}
              </span>
              {cluster === c.cluster && (
                <IconCircleFilled className="size-2.5 text-primary" />
              )}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
