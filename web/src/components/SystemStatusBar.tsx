import React from 'react';
import { Lock, Activity, Server, Zap } from 'lucide-react';
import { SystemStatus } from '../types';

interface SystemStatusBarProps {
  status: SystemStatus;
}

export const SystemStatusBar: React.FC<SystemStatusBarProps> = ({ status }) => {
  return (
    <div className="metrics-row">
      <div className="metric-card">
        <div className="metric-label">
          <Lock size={16} style={{ color: '#6366f1' }} />
          Active Locks
        </div>
        <div className="metric-value">{status.active_locks_count}</div>
      </div>

      <div className="metric-card">
        <div className="metric-label">
          <Server size={16} style={{ color: '#10b981' }} />
          etcd Cluster Quorum
        </div>
        <div className="metric-value" style={{ color: status.etcd_health === 'healthy' ? '#34d399' : '#f87171' }}>
          {status.etcd_health === 'healthy' ? '3 / 3 Nodes (Healthy)' : 'Quorum Partitioned'}
        </div>
      </div>

      <div className="metric-card">
        <div className="metric-label">
          <Zap size={16} style={{ color: '#f59e0b' }} />
          Redis Cache
        </div>
        <div className="metric-value" style={{ color: status.redis_health === 'healthy' ? '#fbbf24' : '#9ca3af' }}>
          {status.redis_health === 'healthy' ? 'Online (Read Cache)' : 'Standalone Mode'}
        </div>
      </div>

      <div className="metric-card">
        <div className="metric-label">
          <Activity size={16} style={{ color: '#ec4899' }} />
          Audit Events
        </div>
        <div className="metric-value">{status.total_events}</div>
      </div>
    </div>
  );
};
