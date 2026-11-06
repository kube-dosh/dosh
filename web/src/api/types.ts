export interface User {
  username: string;
  groups?: string[];
  admin: boolean;
}

export interface Capabilities {
  read: boolean;
  scale: boolean;
  restart: boolean;
  updateImage: boolean;
  suspendCronJob: boolean;
  delete: boolean;
  create: boolean;
  createService: boolean;
  updateService: boolean;
  deleteService: boolean;
  deleteNamespace: boolean;
  revealSecrets: boolean;
  editYAML: boolean;
  nodeCordon: boolean;
  reloadCertificate: boolean;
}

export interface ServerCertificate {
  mode: "https" | "http";
  enabled: boolean;
  configured: boolean;
  status: "valid" | "expiring" | "expired" | "not_yet_valid" | "disabled" | "unavailable";
  subject?: string;
  issuer?: string;
  serialNumber?: string;
  dnsNames: string[];
  ipAddresses: string[];
  notBefore?: string;
  notAfter?: string;
  daysRemaining: number;
  fingerprintSha256?: string;
  keyAlgorithm?: string;
  autoReload: boolean;
  reloadIntervalSeconds: number;
  lastReloadedAt?: string;
  lastReloadError?: string;
}

export interface SessionResponse {
  user: User;
  csrfToken: string;
  expiresAt: string;
  capabilities: Capabilities;
  authMode: "local" | "oidc" | "token";
}

export interface AuthConfig {
  mode: "local" | "oidc" | "token";
  providerName?: string;
}

export interface ListMeta {
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  sort?: string;
  order?: "asc" | "desc";
  filters?: Record<string, string>;
  namespace?: string;
  search?: string;
  kind?: string;
  status?: string;
}

export interface ListResponse<T> {
  items: T[];
  meta: ListMeta;
}

export interface Problem {
  code: string;
  message: string;
  requestId?: string;
  fieldErrors?: Record<string, string>;
}

export interface Condition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface Taint {
  key: string;
  value?: string;
  effect: string;
  timeAdded?: string;
}

export interface ClusterInfo {
  name: string;
  version: string;
  platform?: string;
  cni?: string;
  status: string;
  metricsAvailable: boolean;
}

export interface OverviewCounts {
  nodes: number;
  readyNodes: number;
  pods: number;
  runningPods: number;
  workloads: number;
  namespaces: number;
  unhealthyPods: number;
  warningEvents: number;
}

export interface ClusterCapacity {
  cpuAllocatableMilli: number;
  memoryAllocatableBytes: number;
  podCapacity: number;
  cpuRequestsMilli: number;
  memoryRequestsBytes: number;
  cpuLimitsMilli: number;
  memoryLimitsBytes: number;
  cpuUsageMilli: number;
  memoryUsageBytes: number;
}

export interface EventItem {
  uid: string;
  namespace: string;
  type: string;
  reason: string;
  message: string;
  objectKind: string;
  objectName: string;
  count: number;
  firstSeen: string;
  lastSeen: string;
  source?: string;
}

export interface AuditRecord {
  id: number;
  timestamp: string;
  user: string;
  groups?: string[];
  action: string;
  resource: string;
  namespace?: string;
  name?: string;
  result: "success" | "failure" | "denied";
  requestId?: string;
  error?: string;
}

export interface Overview {
  cluster: ClusterInfo;
  counts: OverviewCounts;
  capacity: ClusterCapacity;
  recentEvents: EventItem[];
  warnings?: string[];
}

export interface NodeSummary {
  name: string;
  status: string;
  roles: string[];
  internalIP: string;
  version: string;
  osImage: string;
  kernelVersion: string;
  containerRuntime: string;
  cpuCapacityMilli: number;
  memoryCapacityBytes: number;
  podCapacity: number;
  podCount: number;
  cpuRequestsMilli: number;
  memoryRequestsBytes: number;
  cpuUsageMilli?: number;
  memoryUsageBytes?: number;
  hasMetrics: boolean;
  unschedulable: boolean;
  createdAt: string;
}

export interface NodeDetail extends NodeSummary {
  resourceVersion: string;
  conditions: Condition[];
  taints: Taint[];
  labels: Record<string, string>;
  annotations: Record<string, string>;
  pods: PodSummary[];
}

export interface NodeLabelUpdate {
  labels: Record<string, string>;
  resourceVersion: string;
}

export interface NodeTaintUpdate {
  taints: Taint[];
  resourceVersion: string;
}

export interface NamespaceItem {
  name: string;
  status: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  createdAt: string;
}

export interface ResourceQuota {
  name: string;
  hard: Record<string, string>;
  used: Record<string, string>;
  createdAt: string;
}

export interface LimitRangeItem {
  type: string;
  min: Record<string, string>;
  max: Record<string, string>;
  default: Record<string, string>;
  defaultRequest: Record<string, string>;
}

export interface LimitRange {
  name: string;
  limits: LimitRangeItem[];
  createdAt: string;
}

export interface NamespaceDetail extends NamespaceItem {
  resourceQuotas: ResourceQuota[];
  limitRanges: LimitRange[];
}

export interface WorkloadSummary {
  kind: string;
  name: string;
  namespace: string;
  resourceVersion?: string;
  status: string;
  ready: string;
  desired: number;
  restarts: number;
  images: string[];
  createdAt: string;
  labels?: Record<string, string>;
}

export interface WorkloadDetail extends WorkloadSummary {
  conditions: Condition[];
  selector: Record<string, string>;
  pods: PodSummary[];
  containers: WorkloadContainer[];
}

export interface WorkloadContainer {
  name: string;
  image: string;
}

export interface WorkloadImageUpdateInput {
  containerName: string;
  image: string;
  resourceVersion?: string;
}

export interface CronJobSuspendInput {
  suspend: boolean;
  resourceVersion?: string;
}

export interface DeploymentEnvVar {
  name: string;
  value: string;
}

export interface DeploymentCreateInput {
  name: string;
  namespace: string;
  replicas: number;
  containerName?: string;
  image: string;
  containerPort: number;
  cpuRequestMilli?: number;
  cpuLimitMilli?: number;
  memoryRequestBytes?: number;
  memoryLimitBytes?: number;
  env?: DeploymentEnvVar[];
  labels?: Record<string, string>;
}

export interface DeploymentPreview {
  yaml: string;
}

export interface ContainerInfo {
  name: string;
  image: string;
  ready: boolean;
  restartCount: number;
  state: string;
  reason?: string;
  cpuRequestMilli: number;
  cpuLimitMilli: number;
  memoryRequestBytes: number;
  memoryLimitBytes: number;
}

export interface PodSummary {
  name: string;
  namespace: string;
  status: string;
  ready: string;
  restarts: number;
  ip: string;
  node: string;
  ownerKind?: string;
  ownerName?: string;
  createdAt: string;
  labels?: Record<string, string>;
  cpuUsageMilli?: number;
  memoryUsageBytes?: number;
  hasMetrics: boolean;
}

export interface PodDetail extends PodSummary {
  containers: ContainerInfo[];
  conditions: Condition[];
  qosClass: string;
  serviceAccount: string;
}

export interface ServicePort {
  name?: string;
  protocol: string;
  port: number;
  targetPort: string;
  nodePort?: number;
}

export interface ServiceSummary {
  name: string;
  namespace: string;
  resourceVersion?: string;
  type: string;
  clusterIP: string;
  externalIP: string;
  ports: ServicePort[];
  selector?: Record<string, string>;
  endpointCount: number;
  createdAt: string;
}

export interface ServicePortInput {
  name?: string;
  protocol?: string;
  port: number;
  targetPort?: string;
}

export interface ServiceCreateInput {
  name: string;
  namespace: string;
  type: string;
  selector: Record<string, string>;
  ports: ServicePortInput[];
}

export interface ServiceUpdateInput {
  type: string;
  selector: Record<string, string>;
  ports: ServicePortInput[];
  resourceVersion?: string;
}

export interface ServiceEndpoint {
  addresses: string[];
  ready: boolean;
  targetKind?: string;
  targetName?: string;
  targetNamespace?: string;
  nodeName?: string;
}

export interface ServiceDetail extends ServiceSummary {
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  externalName?: string;
  sessionAffinity: string;
  ipFamilies: string[];
  ipFamilyPolicy?: string;
  loadBalancerClass?: string;
  endpoints: ServiceEndpoint[];
}

export interface IngressPath {
  host: string;
  path: string;
  pathType: string;
  service: string;
  port: string;
  targetStatus: "Ready" | "NoReadyEndpoints" | "ServiceNotFound" | "PortNotFound" | "Unknown";
}

export interface IngressSummary {
  name: string;
  namespace: string;
  class: string;
  paths: IngressPath[];
  tlsHosts?: string[];
  createdAt: string;
}

export interface IngressTLS {
  hosts: string[];
  secretName?: string;
}

export interface IngressDetail extends IngressSummary {
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  defaultBackend?: IngressPath;
  tls?: IngressTLS[];
}

export interface IngressClassSummary {
  name: string;
  controller: string;
  default: boolean;
  createdAt: string;
}

export interface ConfigMapSummary {
  name: string;
  namespace: string;
  keys: string[];
  dataBytes: number;
  createdAt: string;
}

export interface ConfigMapDetail extends ConfigMapSummary {
  data: Record<string, string>;
}

export interface SecretKey {
  key: string;
  size: number;
}

export interface SecretSummary {
  name: string;
  namespace: string;
  type: string;
  keys: SecretKey[];
  createdAt: string;
}

export interface SecretReveal extends SecretSummary {
  data: Record<string, string>;
}

export interface StorageClass {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBinding: string;
  default: boolean;
  createdAt: string;
}

export interface StorageClassDetail extends StorageClass {
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  parameters?: Record<string, string>;
  mountOptions?: string[];
  allowVolumeExpansion: boolean;
}

export interface PersistentVolume {
  name: string;
  capacityBytes: number;
  accessModes: string[];
  reclaimPolicy: string;
  status: string;
  claim?: string;
  storageClass: string;
  createdAt: string;
}

export interface PersistentVolumeDetail extends PersistentVolume {
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  volumeMode: string;
  mountOptions?: string[];
  claimNamespace?: string;
  claimName?: string;
  csiDriver?: string;
}

export interface PersistentVolumeClaim {
  name: string;
  namespace: string;
  status: string;
  volumeName?: string;
  capacityBytes: number;
  accessModes: string[];
  storageClass: string;
  createdAt: string;
}

export interface PersistentVolumeClaimDetail extends PersistentVolumeClaim {
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  volumeMode: string;
  requestedCapacityBytes: number;
  conditions: Condition[];
}

export interface YAMLResource {
  kind: string;
  namespace?: string;
  name: string;
  yaml: string;
}

export type WorkloadKind = "deployment" | "statefulset" | "daemonset" | "job" | "cronjob";

export interface ListParams {
  namespace?: string;
  q?: string;
  kind?: string;
  status?: string;
  action?: string;
  result?: string;
  objectKind?: string;
  objectName?: string;
  page?: number;
  pageSize?: number;
  sort?: string;
  order?: "asc" | "desc";
}

