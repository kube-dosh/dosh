import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { Toaster } from "sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { initializeTheme, ThemeProvider, useTheme } from "@/lib/theme";
import { router } from "@/router";
import "@/index.css";

initializeTheme();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
    mutations: { retry: 0 },
  },
});

const root = document.getElementById("root");
if (!root) throw new Error("root element is missing");

function ThemedToaster() {
  const { theme } = useTheme();
  return <Toaster richColors position="top-right" closeButton theme={theme} />;
}

createRoot(root).render(
  <StrictMode>
    <ThemeProvider>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={250}>
        <RouterProvider router={router} />
        <ThemedToaster />
      </TooltipProvider>
    </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
