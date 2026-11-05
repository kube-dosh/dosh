import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { api, onUnauthorized } from "@/api/client";
import type { Capabilities, SessionResponse } from "@/api/types";

export const sessionQueryKey = ["session"] as const;

export function useUnauthorizedRedirect() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  useEffect(() => onUnauthorized(() => {
    if (pathname === "/login") return;
    queryClient.clear();
    void navigate({ to: "/login", replace: true });
  }), [navigate, pathname, queryClient]);
}

export function useSessionQuery() {
  return useQuery<SessionResponse>({
    queryKey: sessionQueryKey,
    queryFn: api.session,
    staleTime: 30_000,
    retry: false,
  });
}

export function useCapabilities(namespace?: string) {
  const session = useSessionQuery();
  return useQuery<Capabilities>({
    queryKey: ["capabilities", namespace ?? ""],
    queryFn: () => api.capabilities(namespace),
    enabled: Boolean(session.data),
    initialData: namespace ? undefined : session.data?.capabilities,
    staleTime: 30_000,
  });
}

