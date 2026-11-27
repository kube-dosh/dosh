import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, onUnauthorized } from "@/api/client";

describe("api request errors", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("turns a transport failure into an actionable network error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    await expect(api.nodes()).rejects.toMatchObject({
      status: 0,
      code: "network_error",
      message: "网络连接失败，请检查网络后重试",
    } satisfies Partial<ApiError>);
  });

  it("reports an invalid JSON response instead of leaking a parse error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("not-json", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(api.nodes()).rejects.toMatchObject({
      status: 200,
      code: "invalid_response",
    } satisfies Partial<ApiError>);
  });

  it("localizes stable API error codes while retaining request metadata", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "forbidden",
      message: "operation is not permitted",
      requestId: "req-403",
    }), { status: 403, headers: { "Content-Type": "application/json" } })));

    await expect(api.nodes()).rejects.toMatchObject({
      status: 403,
      code: "forbidden",
      message: "没有执行该操作的权限",
      detail: "operation is not permitted",
      requestId: "req-403",
    } satisfies Partial<ApiError>);
  });

  it("keeps the Dosh session when an upstream registry rejects authentication", async () => {
    const unauthorizedHandler = vi.fn();
    const unsubscribe = onUnauthorized(unauthorizedHandler);
    try {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
        code: "dependency_unauthorized",
        message: "镜像仓要求认证或当前凭证无效",
        requestId: "req-registry-auth",
      }), { status: 401, headers: { "Content-Type": "application/json" } })));

      await expect(api.containerRegistryRepositories("protected")).rejects.toMatchObject({
        status: 401,
        code: "dependency_unauthorized",
        message: "镜像仓认证失败，请检查镜像仓凭证配置",
        detail: "镜像仓要求认证或当前凭证无效",
        requestId: "req-registry-auth",
      } satisfies Partial<ApiError>);
      expect(unauthorizedHandler).not.toHaveBeenCalled();
    } finally {
      unsubscribe();
    }
  });

  it("signals an expired session only for a genuine Dosh unauthorized response", async () => {
    const unauthorizedHandler = vi.fn();
    const unsubscribe = onUnauthorized(unauthorizedHandler);
    try {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
        code: "unauthorized",
        message: "authentication required",
      }), { status: 401, headers: { "Content-Type": "application/json" } })));

      await expect(api.nodes()).rejects.toMatchObject({ status: 401, code: "unauthorized" });
      expect(unauthorizedHandler).toHaveBeenCalledTimes(1);
    } finally {
      unsubscribe();
    }
  });

  it("keeps actionable not-supported guidance returned by the API", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "not_supported",
      message: "集群未安装 autoscaling.k8s.io/v1 VerticalPodAutoscaler CRD，请先安装 VPA 控制器与 CRD",
      requestId: "req-501",
    }), { status: 501, headers: { "Content-Type": "application/json" } })));

    await expect(api.nodes()).rejects.toMatchObject({
      status: 501,
      code: "not_supported",
      message: "集群未安装 autoscaling.k8s.io/v1 VerticalPodAutoscaler CRD，请先安装 VPA 控制器与 CRD",
      requestId: "req-501",
    } satisfies Partial<ApiError>);
  });

  it("normalizes nullable certificate SAN arrays before rendering", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      mode: "https", enabled: true, configured: true, status: "valid",
      subject: "CN=dosh.example.test", issuer: "CN=test", serialNumber: "01",
      dnsNames: null, ipAddresses: null,
      notBefore: "2026-01-01T00:00:00Z", notAfter: "2027-01-01T00:00:00Z",
      daysRemaining: 365, fingerprintSha256: "", keyAlgorithm: "ECDSA P-256",
      autoReload: true, reloadIntervalSeconds: 30,
    }), { status: 200, headers: { "Content-Type": "application/json" } })));

    const certificate = await api.serverCertificate();

    expect(certificate.dnsNames).toEqual([]);
    expect(certificate.ipAddresses).toEqual([]);
  });

  it("normalizes nullable list payloads to safe empty collections", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: null }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    const response = await api.nodes();

    expect(response.items).toEqual([]);
    expect(response.meta.total).toBe(0);
    expect(response.meta.page).toBe(1);
  });

  it("normalizes nullable overview and cluster configuration arrays", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        cluster: {}, counts: {}, capacity: {}, recentEvents: null, warnings: null,
      }), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        name: "test", nodeCIDRs: [{ name: "worker-1", podCIDRs: null }], warnings: null,
      }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const overview = await api.overview();
    const configuration = await api.clusterConfiguration();

    expect(overview.recentEvents).toEqual([]);
    expect(overview.warnings).toEqual([]);
    expect(configuration.nodeCIDRs[0].podCIDRs).toEqual([]);
    expect(configuration.warnings).toEqual([]);
  });
});
