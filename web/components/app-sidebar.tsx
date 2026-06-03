"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  IconActivity,
  IconApi,
  IconBox,
  IconDashboard,
  IconDatabase,
  IconFlask,
  IconRoute,
  IconScript,
  IconServer,
  IconSettings,
  IconWaveSine,
  type Icon,
} from "@tabler/icons-react";

import { NavUser } from "@/components/nav-user";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useT } from "@/lib/i18n";

type NavLink = {
  href: string;
  labelKey: string;
  icon: Icon;
};

const platform: NavLink[] = [
  { href: "/", labelKey: "nav.overview", icon: IconDashboard },
  { href: "/engines", labelKey: "nav.engines", icon: IconServer },
  { href: "/profiles", labelKey: "nav.profiles", icon: IconBox },
  { href: "/policies", labelKey: "nav.policies", icon: IconRoute },
];

const observability: NavLink[] = [
  { href: "/streams", labelKey: "nav.streams", icon: IconWaveSine },
  { href: "/decisions", labelKey: "nav.decisions", icon: IconActivity },
  { href: "/audit", labelKey: "nav.audit", icon: IconScript },
];

const tools: NavLink[] = [
  { href: "/simulator", labelKey: "nav.simulator", icon: IconFlask },
  { href: "/api-docs", labelKey: "nav.api_docs", icon: IconApi },
];

function isActive(path: string, href: string) {
  return href === "/" ? path === "/" : path.startsWith(href);
}

function NavSection({
  label,
  items,
  pathname,
  t,
}: {
  label: string;
  items: NavLink[];
  pathname: string;
  t: (k: string) => string;
}) {
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => {
            const active = isActive(pathname, item.href);
            const Icon = item.icon;
            return (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton
                  asChild
                  tooltip={t(item.labelKey)}
                  isActive={active}
                >
                  <Link href={item.href}>
                    <Icon />
                    <span>{t(item.labelKey)}</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            );
          })}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname();
  const t = useT();

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:p-1.5!"
            >
              <Link href="/">
                <IconDatabase className="size-5!" />
                <span className="text-base font-semibold">
                  {t("app.brand")}
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavSection
          label={t("app.section.platform")}
          items={platform}
          pathname={pathname}
          t={t}
        />
        <NavSection
          label={t("app.section.observability")}
          items={observability}
          pathname={pathname}
          t={t}
        />
        <NavSection
          label={t("app.section.tools")}
          items={tools}
          pathname={pathname}
          t={t}
        />
      </SidebarContent>
      <SidebarFooter>
        <NavUser
          user={{
            name: "operator",
            email: "ops@kv-indexer",
            avatar: "",
          }}
        />
      </SidebarFooter>
    </Sidebar>
  );
}
