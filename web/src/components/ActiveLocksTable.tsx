import React from 'react';
import { Lock, Unlock, RefreshCw, Hash, Shield } from 'lucide-react';
import { LockMetadata } from '../types';

interface ActiveLocksTableProps {
  locks: LockMetadata[];
  onRelease: (resourceId: string, clientId: string, leaseId: number, fencingToken: number) => Promise<void>;
  onRenew: (resourceId: string, clientId: string, leaseId: number, ttl: number) => Promise<void>;
}

export const ActiveLocksTable: React.FC<ActiveLocksTableProps> = ({ locks, onRelease, onRenew }) => {
  return (
    <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ fontSize: '16px', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Lock size={18} style={{ color: 'var(--accent-primary)' }} />
          Active Distributed Locks ({locks.length})
        </h2>
        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
          Linearizable Raft MVCC Leases
        </span>
      </div>

      <div className="table-container">
        {locks.length === 0 ? (
          <div style={{ padding: '36px', textAlign: 'center', color: 'var(--text-secondary)' }}>
            No active locks currently held. Use the control card above or run a chaos test to acquire one!
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Resource ID</th>
                <th>Owner Client</th>
                <th>Fencing Token</th>
                <th>Lease ID</th>
                <th>TTL Remaining</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {locks.map((lock) => {
                const totalTtl = lock.ttl_seconds || 10;
                const remaining = lock.ttl_remaining_seconds ?? 0;
                const pct = Math.max(0, Math.min(100, (remaining / totalTtl) * 100));

                return (
                  <tr key={lock.resource_id}>
                    <td style={{ fontWeight: 600, color: 'var(--text-primary)' }}>
                      {lock.resource_id}
                    </td>
                    <td>
                      <span style={{ fontFamily: 'var(--font-mono)', color: '#93c5fd' }}>
                        {lock.owner_id}
                      </span>
                    </td>
                    <td>
                      <span className="fencing-badge">
                        <Shield size={12} />
                        #{lock.fencing_token}
                      </span>
                    </td>
                    <td style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-muted)', fontSize: '12px' }}>
                      {lock.lease_id}
                    </td>
                    <td style={{ minWidth: '130px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '12px', fontFamily: 'var(--font-mono)' }}>
                        <span>{remaining}s</span>
                        <span style={{ color: 'var(--text-muted)' }}>/ {totalTtl}s</span>
                      </div>
                      <div className="ttl-bar-container">
                        <div className="ttl-bar" style={{ width: `${pct}%` }} />
                      </div>
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '6px' }}>
                        <button
                          type="button"
                          className="btn btn-warning"
                          style={{ padding: '6px 10px', fontSize: '11px' }}
                          title="Heartbeat Keep-Alive (+10s)"
                          onClick={() => onRenew(lock.resource_id, lock.owner_id, lock.lease_id, 10)}
                        >
                          <RefreshCw size={12} />
                          Renew
                        </button>
                        <button
                          type="button"
                          className="btn btn-danger"
                          style={{ padding: '6px 10px', fontSize: '11px' }}
                          title="Release Lock & Revoke Lease"
                          onClick={() => onRelease(lock.resource_id, lock.owner_id, lock.lease_id, lock.fencing_token)}
                        >
                          <Unlock size={12} />
                          Release
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
};
