import React from 'react';
import { ShieldAlert, ShieldCheck, TrendingUp, GitCommit } from 'lucide-react';
import { LockEvent } from '../types';

interface FencingTimelineProps {
  events: LockEvent[];
}

export const FencingTimeline: React.FC<FencingTimelineProps> = ({ events }) => {
  // Extract token-related events
  const tokenEvents = events
    .filter((e) => e.fencing_token > 0)
    .slice(0, 12);

  return (
    <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ fontSize: '16px', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '8px' }}>
          <TrendingUp size={18} style={{ color: 'var(--accent-purple)' }} />
          Fencing Token Monotonic Progression (Kleppmann Safety Guard)
        </h2>
        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
          Strict Ordering Invariant: Token(t+1) &gt; Token(t)
        </span>
      </div>

      {tokenEvents.length === 0 ? (
        <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary)' }}>
          No fencing tokens issued yet. Acquire a lock to begin token sequence tracking.
        </div>
      ) : (
        <div style={{ display: 'flex', gap: '10px', overflowX: 'auto', paddingBottom: '8px' }}>
          {tokenEvents.map((ev, i) => {
            const evTypeStr = String(ev.event_type || '').toUpperCase();
            const isStale = evTypeStr.includes('STALE') || evTypeStr.includes('REJECTED');
            const isAccepted = evTypeStr.includes('ACCEPTED') || evTypeStr.includes('WRITE');

            let borderColor = 'rgba(139, 92, 246, 0.4)';
            let bgColor = 'rgba(139, 92, 246, 0.1)';
            let icon = <GitCommit size={14} style={{ color: '#c4b5fd' }} />;

            if (isStale) {
              borderColor = 'rgba(239, 68, 68, 0.6)';
              bgColor = 'rgba(239, 68, 68, 0.15)';
              icon = <ShieldAlert size={14} style={{ color: '#f87171' }} />;
            } else if (isAccepted) {
              borderColor = 'rgba(16, 185, 129, 0.6)';
              bgColor = 'rgba(16, 185, 129, 0.15)';
              icon = <ShieldCheck size={14} style={{ color: '#34d399' }} />;
            }

            return (
              <div
                key={ev.event_id || i}
                style={{
                  minWidth: '150px',
                  background: bgColor,
                  border: `1px solid ${borderColor}`,
                  borderRadius: '10px',
                  padding: '12px',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '6px',
                  flexShrink: 0,
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                    {ev.resource_id}
                  </span>
                  {icon}
                </div>

                <div style={{ fontSize: '18px', fontWeight: 800, fontFamily: 'var(--font-mono)', color: '#fff' }}>
                  Token #{ev.fencing_token}
                </div>

                <div style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>
                  By: <strong style={{ color: '#93c5fd' }}>{ev.client_id}</strong>
                </div>

                <div style={{ fontSize: '10px', color: isStale ? '#fca5a5' : isAccepted ? '#6ee7b7' : 'var(--text-muted)', fontWeight: 600 }}>
                  {isStale ? 'STALE WRITE BLOCKED' : isAccepted ? 'WRITE COMMITTED' : 'ISSUED BY RAFT'}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
