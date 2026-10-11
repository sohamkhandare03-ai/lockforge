export interface LockMetadata {
  resource_id: string;
  owner_id: string;
  lease_id: number;
  fencing_token: number;
  acquired_at_unix_ms: number;
  expires_at_unix_ms: number;
  ttl_seconds: number;
  ttl_remaining_seconds?: number;
}

export type LockEventType =
  | 'LOCK_ACQUIRED'
  | 'LOCK_RELEASED'
  | 'LEASE_RENEWED'
  | 'LEASE_EXPIRED'
  | 'STALE_WRITE_REJECTED'
  | 'WRITE_ACCEPTED'
  | 'QUORUM_LOST'
  | 'QUORUM_RESTORED';

export interface LockEvent {
  event_id: string;
  event_type: LockEventType;
  resource_id: string;
  client_id: string;
  fencing_token: number;
  lease_id: number;
  timestamp_unix_ms: number;
  details: string;
}

export interface SystemStatus {
  connected: boolean;
  etcd_health: 'healthy' | 'degraded' | 'offline';
  etcd_nodes: number;
  redis_health: 'healthy' | 'offline';
  active_locks_count: number;
  fencing_counter: number;
  total_events: number;
}

export interface BenchmarkResult {
  engine: string;
  operations: number;
  concurrency: number;
  total_time_ms: number;
  ops_per_sec: number;
  p50_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  success_rate: number;
}
