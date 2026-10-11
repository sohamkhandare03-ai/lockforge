import React, { useState } from 'react';
import { Skull, Users, ShieldAlert, Network, BarChart2, CheckCircle2 } from 'lucide-react';

interface ChaosStudioProps {
  onTriggerChaos: (scenario: string) => Promise<any>;
}

export const ChaosStudio: React.FC<ChaosStudioProps> = ({ onTriggerChaos }) => {
  const [running, setRunning] = useState<string | null>(null);
  const [resultMessage, setResultMessage] = useState<string | null>(null);

  const handleRun = async (scenario: string) => {
    setRunning(scenario);
    setResultMessage(null);
    try {
      const res = await onTriggerChaos(scenario);
      setResultMessage(res.message || 'Chaos scenario executed successfully.');
    } catch (err: any) {
      setResultMessage(`Error executing scenario: ${err.message}`);
    } finally {
      setRunning(null);
    }
  };

  return (
    <div className="glass-panel" style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ fontSize: '16px', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Skull size={18} style={{ color: 'var(--accent-danger)' }} />
          Chaos Engineering &amp; Verification Studio
        </h2>
        <span style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
          Distributed Safety Scenarios
        </span>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '10px' }}>
        <button
          type="button"
          className="btn btn-secondary"
          style={{ justifyContent: 'flex-start', padding: '12px' }}
          disabled={running !== null}
          onClick={() => handleRun('concurrency')}
        >
          <Users size={18} style={{ color: '#60a5fa' }} />
          <div style={{ textAlign: 'left' }}>
            <div style={{ fontWeight: 600 }}>10-Worker Race</div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>Hammer 1 lock concurrently</div>
          </div>
        </button>

        <button
          type="button"
          className="btn btn-secondary"
          style={{ justifyContent: 'flex-start', padding: '12px' }}
          disabled={running !== null}
          onClick={() => handleRun('crash')}
        >
          <Skull size={18} style={{ color: '#f87171' }} />
          <div style={{ textAlign: 'left' }}>
            <div style={{ fontWeight: 600 }}>Owner Crash</div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>Lease auto-expiry &amp; recovery</div>
          </div>
        </button>

        <button
          type="button"
          className="btn btn-secondary"
          style={{ justifyContent: 'flex-start', padding: '12px' }}
          disabled={running !== null}
          onClick={() => handleRun('stale_write')}
        >
          <ShieldAlert size={18} style={{ color: '#c084fc' }} />
          <div style={{ textAlign: 'left' }}>
            <div style={{ fontWeight: 600 }}>Stale Write Attack</div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>Kleppmann fencing guard</div>
          </div>
        </button>

        <button
          type="button"
          className="btn btn-secondary"
          style={{ justifyContent: 'flex-start', padding: '12px' }}
          disabled={running !== null}
          onClick={() => handleRun('partition')}
        >
          <Network size={18} style={{ color: '#f59e0b' }} />
          <div style={{ textAlign: 'left' }}>
            <div style={{ fontWeight: 600 }}>Quorum Partition</div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>Prevent split-brain toggle</div>
          </div>
        </button>
      </div>

      {running && (
        <div style={{ fontSize: '12px', color: '#93c5fd', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="pulse-dot" style={{ background: '#60a5fa' }} />
          Executing chaos injection: {running}...
        </div>
      )}

      {resultMessage && (
        <div
          style={{
            padding: '10px 14px',
            borderRadius: '8px',
            background: 'rgba(99, 102, 241, 0.1)',
            border: '1px solid rgba(99, 102, 241, 0.3)',
            fontSize: '13px',
            color: '#c7d2fe',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <CheckCircle2 size={16} style={{ color: '#818cf8', flexShrink: 0 }} />
          <span>{resultMessage}</span>
        </div>
      )}
    </div>
  );
};
