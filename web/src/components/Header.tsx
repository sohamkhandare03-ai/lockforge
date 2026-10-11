import React from 'react';
import { ShieldCheck, ShieldAlert, Cpu } from 'lucide-react';
import { SystemStatus } from '../types';

interface HeaderProps {
  status: SystemStatus;
  sseConnected: boolean;
}

export const Header: React.FC<HeaderProps> = ({ status, sseConnected }) => {
  return (
    <header className="header-bar">
      <div className="logo-section">
        <div className="logo-icon">
          <ShieldCheck size={26} />
        </div>
        <div>
          <h1 className="logo-title">LockForge</h1>
          <p className="logo-subtitle">Distributed Lock Manager (Raft Consensus &bull; Fencing Tokens &bull; Redis Cache)</p>
        </div>
      </div>

      <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
        <div className={`status-pill ${sseConnected ? 'status-online' : 'status-offline'}`}>
          <span className="pulse-dot" />
          {sseConnected ? 'SSE Live Streaming' : 'Reconnecting...'}
        </div>

        <div className={`status-pill ${status.etcd_health === 'healthy' ? 'status-online' : 'status-offline'}`}>
          <Cpu size={14} />
          etcd: {status.etcd_health.toUpperCase()}
        </div>

        <div className="status-pill status-online" style={{ background: 'rgba(99, 102, 241, 0.15)', color: '#a5b4fc', border: '1px solid rgba(99, 102, 241, 0.3)' }}>
          Fencing Guard: ACTIVE
        </div>
      </div>
    </header>
  );
};
