import type { components } from '../../types/api';
import { Sparkline } from './Sparkline';

type AlertEvidence = components['schemas']['AlertEvidence'];

interface Props {
  readonly evidence: AlertEvidence | undefined;
  readonly loading: boolean;
  /** What the server said when the evidence could not be handed over. */
  readonly error: string | undefined;
}

const HEAD_CELL = 'px-2 py-1 text-left text-xs font-semibold text-gray-500';
const CELL = 'px-2 py-1 text-xs text-gray-300';

function Section({ title, children }: { readonly title: string; readonly children: React.ReactNode }) {
  return (
    <div>
      <h5 className="text-xs font-semibold text-gray-400 mb-1">{title}</h5>
      {children}
    </div>
  );
}

/**
 * Everything the machine knew about why one alert fired, frozen at write time.
 *
 * Nothing here is fetched from the machine: what is not in this snapshot about
 * an event is not recorded anywhere, which is why an absence is stated rather
 * than left as a blank somebody might read as "still loading".
 *
 * Every value on this panel is host-supplied — log lines, process names — so it
 * is rendered as text throughout, and nothing in it becomes a link.
 */
export function AlertEvidencePanel({ evidence, loading, error }: Props) {
  if (error !== undefined) {
    return <p role="note" className="text-xs text-gray-400">{error}</p>;
  }
  if (!evidence) {
    return <p className="text-xs text-gray-400">{loading ? 'Reading the evidence…' : 'No evidence has been read.'}</p>;
  }

  return (
    <div className="space-y-3 bg-gray-900 border border-gray-700 rounded p-3">
      {evidence.truncated && (
        <p role="alert" className="text-xs text-amber-400">
          The size cap cost this evidence some of what the machine recorded.
        </p>
      )}

      <Section title="Ranked dimensions">
        {evidence.ranked.length === 0
          ? <p className="text-xs text-gray-500">No ranked dimensions were recorded.</p>
          : (
            <ul aria-label="Ranked dimensions" className="space-y-0.5">
              {evidence.ranked.map((r) => (
                <li key={r.dim} className="text-xs text-gray-300 flex justify-between gap-3">
                  <span>{r.dim}</span>
                  <span className="tabular-nums text-gray-400">{Number(r.score.toFixed(2))}</span>
                </li>
              ))}
            </ul>
          )}
      </Section>

      <Section title="Series">
        {evidence.series.length === 0
          ? <p className="text-xs text-gray-500">No series were recorded.</p>
          : (
            <div className="space-y-2">
              {evidence.series.map((s) => (
                <div key={s.dim}>
                  <p className="text-xs text-gray-400">{s.dim}</p>
                  <Sparkline dim={s.dim} points={s.points} />
                </div>
              ))}
            </div>
          )}
      </Section>

      <Section title="Processes">
        {evidence.processes.length === 0
          ? <p className="text-xs text-gray-500">No processes were recorded.</p>
          : (
            <table aria-label="Processes" className="w-full">
              <thead>
                <tr>
                  <th className={HEAD_CELL}>#</th>
                  <th className={HEAD_CELL}>Process</th>
                  <th className={HEAD_CELL}>PID</th>
                  <th className={HEAD_CELL}>CPU</th>
                  <th className={HEAD_CELL}>Memory</th>
                </tr>
              </thead>
              <tbody>
                {evidence.processes.map((p) => (
                  <tr key={`${String(p.pid)}-${p.basename}`}>
                    <td className={`${CELL} tabular-nums`}>{p.rank}</td>
                    <td className={CELL}>{p.basename}</td>
                    <td className={`${CELL} tabular-nums`}>{p.pid}</td>
                    <td className={`${CELL} tabular-nums`}>{Number(p.cpu.toFixed(2))}%</td>
                    <td className={`${CELL} tabular-nums`}>{Number(p.mem.toFixed(2))}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
      </Section>

      <Section title="Log lines">
        {evidence.log_samples.length === 0
          ? <p className="text-xs text-gray-500">No log lines were recorded.</p>
          : (
            <ul aria-label="Log lines" className="space-y-1">
              {evidence.log_samples.map((line, i) => (
                <li key={`${String(i)}-${line.slice(0, 32)}`} className="text-xs font-mono text-gray-300 whitespace-pre-wrap wrap-break-word">
                  {line}
                </li>
              ))}
            </ul>
          )}
      </Section>
    </div>
  );
}
