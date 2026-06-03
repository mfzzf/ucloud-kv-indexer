"use client";

import { usePathname } from "next/navigation";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { ThemeToggle } from "@/components/theme-toggle";
import { LocaleToggle } from "@/components/locale-toggle";
import { ClusterSwitcher } from "@/components/cluster-switcher";
import { useT } from "@/lib/i18n";

const NAV_LABEL: Record<string, string> = {
  "/": "nav.overview",
  "/engines": "nav.engines",
  "/profiles": "nav.profiles",
  "/policies": "nav.policies",
  "/streams": "nav.streams",
  "/decisions": "nav.decisions",
  "/audit": "nav.audit",
  "/simulator": "nav.simulator",
};

export function SiteHeader() {
  const path = usePathname();
  const t = useT();

  const matched =
    path === "/"
      ? "/"
      : Object.keys(NAV_LABEL).find((h) => h !== "/" && path.startsWith(h));
  const labelKey = matched ? NAV_LABEL[matched] : "nav.overview";

  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-2 data-[orientation=vertical]:h-4"
        />
        <h1 className="text-base font-medium">{t(labelKey)}</h1>
        <div className="ml-auto flex items-center gap-1">
          <ClusterSwitcher />
          <LocaleToggle />
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}
