import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { Overview } from "@/api/types";

export const overviewQueryKey = ["overview"] as const;

export function useOverviewQuery() {
  return useQuery<Overview>({
    queryKey: overviewQueryKey,
    queryFn: api.overview,
    staleTime: 15_000,
    refetchInterval: 15_000,
  });
}
