import React from 'react';
import { Activity, Radio } from 'lucide-react';
import { LockEvent } from '../types';

interface SystemEventFeedProps {
  events: LockEvent[];
}

export const SystemEventFeed: React.FC<SystemEventFeedProps> = ({ events }) => {
  const getTagClass = (type: any) => {
    const t = String(type || '').toUpperCase();
    if (t.includes('ACQUIRED')) return 'tag-acquire';
    if (t.includes('RELEASED')) return 'tag-release';
    if (t.includes('RENEWED')) return 'tag-renew';
    if (t.includes('EXPIRED')) return 'tag-expire';
    if (t.includes('STALE') || t.includes('REJECTED')) return 'tag-stale';
    if (t.includes('ACCEPTED') || t.includes('WRITE')) return 'tag-write';
    if (t.includes('QUORUM')) return 'tag-quorum';
    return 'tag-acquire';
  };

  const formatTypeName = (type: any) => {
    const t = String(type || '');
    return t.replace(/^LOCK_EVENT_TYPE_/, '').replace(/_/g, ' ') || 'EVENT';
  };

  return (
    <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', gap: '14px', height: '100%' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ fontSize: '16px', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Activity size={18} style={{ color: 'var(--accent-success)' }} />
          Live Audit Stream
        </h2>
        <span style={{ fontSize: '12px', color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '4px' }}>
          <Radio size={12} style={{ color: '#34d399' }} />
          SSE Feed
        </span>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', maxHeight: '520px', overflowY: 'auto' }}>
        {events.length === 0 ? (
          <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-muted)' }}>
            Waiting for cluster events...
          </div>
        ) : (
          events.map((ev, i) => {
            const timeStr = ev.timestamp_unix_ms
              ? new Date(ev.timestamp_unix_ms).toLocaleTimeString()
              : new Date().toLocaleTimeString();

            return (
              <div key={ev.event_id || i} className="event-item">
                <span className={`event-tag ${getTagClass(ev.event_type)}`}>
                  {formatTypeName(ev.event_type)}
                </span>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '2px', flex: 1 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)' }}>
                    <span>
                      Res: <strong style={{ color: 'var(--text-primary)' }}>{ev.resource_id}</strong>
                    </span>
                    <span>{timeStr}</span>
                  </div>

                  <p style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                    {ev.details}
                  </p>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
