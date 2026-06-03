"use client";

import { ThemeProvider } from "next-themes";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { useState } from "react";
import { I18nProvider } from "@/lib/i18n";
import { ClusterProvider } from "@/lib/cluster";

export function Providers({ children }: { children: React.ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { refetchInterval: 4000, staleTime: 2000 },
        },
      }),
  );
  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
    >
      <I18nProvider>
        <QueryClientProvider client={client}>
          <ClusterProvider>
            <TooltipProvider delayDuration={150}>{children}</TooltipProvider>
            <Toaster richColors closeButton />
          </ClusterProvider>
        </QueryClientProvider>
      </I18nProvider>
    </ThemeProvider>
  );
}
