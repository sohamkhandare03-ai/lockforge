import React, { useEffect, useState, useCallback } from 'react';
import { Header } from './components/Header';
import { SystemStatusBar } from './components/SystemStatusBar';
import { LockControlCard } from './components/LockControlCard';
import { ActiveLocksTable } from './components/ActiveLocksTable';
import { FencingTimeline } from './components/FencingTimeline';
import { ChaosStudio } from './components/ChaosStudio';
import { SystemEventFeed } from './components/SystemEventFeed';
import { LockMetadata, LockEvent, SystemStatus } from './types';

export const App: React.FC = () => {
  const [status, setStatus] = useState<SystemStatus>({
    connected: false,
    etcd_health: 'healthy',
    etcd_nodes: 3,
    redis_health: 'healthy',
    active_locks_count: 0,
    fencing_counter: 100,
    total_events: 0,
  });

  const [locks, setLocks] = useState<LockMetadata[]>([]);
  const [events, setEvents] = useState<LockEvent[]>([]);
  const [sseConnected, setSseConnected] = useState(false);

  const fetchLocksAndStatus = useCallback(async () => {
    try {
      const [statusRes, locksRes] = await Promise.all([
        fetch('/api/status'),
        fetch('/api/locks'),
      ]);

      if (statusRes.ok) {
        const sData = await statusRes.json();
        setStatus(sData);
      }

      if (locksRes.ok) {
        const lData = await locksRes.json();
        setLocks(lData || []);
      }
    } catch {
      // Offline fallback
    }
  }, []);

  // Initial load and periodic refresh for countdowns
  useEffect(() => {
    fetchLocksAndStatus();
    const interval = setInterval(fetchLocksAndStatus, 1000);
    return () => clearInterval(interval);
  }, [fetchLocksAndStatus]);

  // Connect to SSE stream
  useEffect(() => {
    let eventSource: EventSource | null = null;

    const connectSSE = () => {
      eventSource = new EventSource('/api/events/stream');

      eventSource.onopen = () => {
        setSseConnected(true);
      };

      eventSource.onmessage = (e) => {
        try {
          const newEvent: LockEvent = JSON.parse(e.data);
          if (newEvent && newEvent.event_type) {
            setEvents((prev) => [newEvent, ...prev].slice(0, 100));
            fetchLocksAndStatus();
          }
        } catch {
          // Keep-alive ping or unparseable
        }
      };

      eventSource.onerror = () => {
        setSseConnected(false);
        eventSource?.close();
        setTimeout(connectSSE, 3000);
      };
    };

    connectSSE();
    return () => {
      eventSource?.close();
    };
  }, [fetchLocksAndStatus]);

  // API handlers
  const handleAcquire = async (resourceId: string, clientId: string, ttl: number) => {
    const res = await fetch('/api/locks/acquire', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resource_id: resourceId, client_id: clientId, ttl_seconds: ttl }),
    });
    const data = await res.json();
    if (!res.ok || !data.acquired) {
      alert(`Acquisition failed: ${data.error_message || 'Key already held'}`);
    }
    fetchLocksAndStatus();
  };

  const handleRelease = async (resourceId: string, clientId: string, leaseId: number, fencingToken: number) => {
    const res = await fetch('/api/locks/release', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: resourceId,
        client_id: clientId,
        lease_id: leaseId,
        fencing_token: fencingToken,
      }),
    });
    const data = await res.json();
    if (!res.ok || !data.released) {
      alert(`Release failed: ${data.message}`);
    }
    fetchLocksAndStatus();
  };

  const handleRenew = async (resourceId: string, clientId: string, leaseId: number, ttl: number) => {
    const res = await fetch('/api/locks/renew', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: resourceId,
        client_id: clientId,
        lease_id: leaseId,
        ttl_seconds: ttl,
      }),
    });
    const data = await res.json();
    if (!res.ok || !data.renewed) {
      alert(`Renewal failed: ${data.message}`);
    }
    fetchLocksAndStatus();
  };

  const handleWriteProtected = async (resourceId: string, clientId: string, token: number, data: string) => {
    const res = await fetch('/api/resource/write', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: resourceId,
        client_id: clientId,
        fencing_token: token,
        data,
      }),
    });
    const resData = await res.json();
    fetchLocksAndStatus();
    return resData;
  };

  const handleTriggerChaos = async (scenario: string) => {
    const res = await fetch(`/api/chaos/${scenario}`, {
      method: 'POST',
    });
    const data = await res.json();
    fetchLocksAndStatus();
    return data;
  };

  return (
    <div className="app-container">
      <Header status={status} sseConnected={sseConnected} />
      <SystemStatusBar status={status} />

      <div className="dashboard-grid">
        <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          <LockControlCard
            onAcquire={handleAcquire}
            onWriteProtected={handleWriteProtected}
          />
          <ActiveLocksTable
            locks={locks}
            onRelease={handleRelease}
            onRenew={handleRenew}
          />
          <FencingTimeline events={events} />
          <ChaosStudio onTriggerChaos={handleTriggerChaos} />
        </div>

        <div>
          <SystemEventFeed events={events} />
        </div>
      </div>
    </div>
  );
};
