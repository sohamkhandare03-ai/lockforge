import React, { useState } from 'react';
import { KeyRound, Send, AlertCircle, CheckCircle2 } from 'lucide-react';

interface LockControlCardProps {
  onAcquire: (resourceId: string, clientId: string, ttl: number) => Promise<void>;
  onWriteProtected: (resourceId: string, clientId: string, token: number, data: string) => Promise<any>;
}

export const LockControlCard: React.FC<LockControlCardProps> = ({ onAcquire, onWriteProtected }) => {
  const [activeTab, setActiveTab] = useState<'acquire' | 'write'>('acquire');

  // Acquire form state
  const [resourceId, setResourceId] = useState('inventory-sync');
  const [clientId, setClientId] = useState('worker-alpha');
  const [ttl, setTtl] = useState(15);
  const [acquireLoading, setAcquireLoading] = useState(false);

  // Protected write state
  const [writeResourceId, setWriteResourceId] = useState('inventory-sync');
  const [writeClientId, setWriteClientId] = useState('worker-alpha');
  const [writeToken, setWriteToken] = useState(101);
  const [writeData, setWriteData] = useState('{"qty": 42}');
  const [writeLoading, setWriteLoading] = useState(false);
  const [writeFeedback, setWriteFeedback] = useState<{ accepted: boolean; message: string } | null>(null);

  const handleAcquireSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setAcquireLoading(true);
    try {
      await onAcquire(resourceId, clientId, ttl);
    } finally {
      setAcquireLoading(false);
    }
  };

  const handleWriteSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setWriteLoading(true);
    setWriteFeedback(null);
    try {
      const res = await onWriteProtected(writeResourceId, writeClientId, writeToken, writeData);
      setWriteFeedback({
        accepted: res.accepted,
        message: res.message || (res.accepted ? 'Write accepted!' : 'Write rejected!'),
      });
    } catch (err: any) {
      setWriteFeedback({
        accepted: false,
        message: err.message || 'Network or conflict error',
      });
    } finally {
      setWriteLoading(false);
    }
  };

  return (
    <div className="glass-panel">
      <div style={{ display: 'flex', gap: '8px', borderBottom: '1px solid var(--border-subtle)', paddingBottom: '12px', marginBottom: '16px' }}>
        <button
          type="button"
          onClick={() => setActiveTab('acquire')}
          className="btn"
          style={{
            background: activeTab === 'acquire' ? 'var(--accent-primary)' : 'transparent',
            color: activeTab === 'acquire' ? '#fff' : 'var(--text-secondary)',
          }}
        >
          <KeyRound size={16} />
          Acquire Lock
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('write')}
          className="btn"
          style={{
            background: activeTab === 'write' ? 'var(--accent-purple)' : 'transparent',
            color: activeTab === 'write' ? '#fff' : 'var(--text-secondary)',
          }}
        >
          <Send size={16} />
          Protected Resource Write
        </button>
      </div>

      {activeTab === 'acquire' ? (
        <form onSubmit={handleAcquireSubmit}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '12px' }}>
            <div className="form-group">
              <label className="form-label">Resource ID</label>
              <input
                type="text"
                className="form-input"
                value={resourceId}
                onChange={(e) => setResourceId(e.target.value)}
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label">Client ID</label>
              <input
                type="text"
                className="form-input"
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label">TTL (seconds)</label>
              <input
                type="number"
                min="1"
                max="300"
                className="form-input"
                value={ttl}
                onChange={(e) => setTtl(Number(e.target.value))}
                required
              />
            </div>
          </div>

          <button
            type="submit"
            className="btn btn-primary"
            style={{ width: '100%', marginTop: '8px' }}
            disabled={acquireLoading}
          >
            {acquireLoading ? 'Acquiring Lock in Raft...' : 'Acquire Distributed Lock'}
          </button>
        </form>
      ) : (
        <form onSubmit={handleWriteSubmit}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '12px' }}>
            <div className="form-group">
              <label className="form-label">Resource ID</label>
              <input
                type="text"
                className="form-input"
                value={writeResourceId}
                onChange={(e) => setWriteResourceId(e.target.value)}
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label">Client ID</label>
              <input
                type="text"
                className="form-input"
                value={writeClientId}
                onChange={(e) => setWriteClientId(e.target.value)}
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label">Fencing Token</label>
              <input
                type="number"
                className="form-input"
                value={writeToken}
                onChange={(e) => setWriteToken(Number(e.target.value))}
                required
              />
            </div>
          </div>

          <div className="form-group">
            <label className="form-label">Data Payload</label>
            <input
              type="text"
              className="form-input"
              value={writeData}
              onChange={(e) => setWriteData(e.target.value)}
              required
            />
          </div>

          <button
            type="submit"
            className="btn"
            style={{ width: '100%', background: 'var(--accent-purple)', color: '#fff', marginTop: '4px' }}
            disabled={writeLoading}
          >
            {writeLoading ? 'Validating Token...' : 'Submit Guarded Write'}
          </button>

          {writeFeedback && (
            <div
              style={{
                marginTop: '12px',
                padding: '10px 14px',
                borderRadius: '8px',
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                fontSize: '13px',
                background: writeFeedback.accepted ? 'rgba(16, 185, 129, 0.15)' : 'rgba(239, 68, 68, 0.15)',
                color: writeFeedback.accepted ? '#6ee7b7' : '#fca5a5',
                border: `1px solid ${writeFeedback.accepted ? 'rgba(16, 185, 129, 0.3)' : 'rgba(239, 68, 68, 0.3)'}`,
              }}
            >
              {writeFeedback.accepted ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
              <span>{writeFeedback.message}</span>
            </div>
          )}
        </form>
      )}
    </div>
  );
};
