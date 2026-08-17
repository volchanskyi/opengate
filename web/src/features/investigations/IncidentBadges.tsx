import type { components } from '../../types/api';
import { severityLabel, severityToneClass, statusLabel, statusToneClass } from './incident-lifecycle';

type Severity = components['schemas']['IncidentSeverity'];
type Status = components['schemas']['IncidentStatus'];

const PILL = 'px-2 py-0.5 rounded text-xs font-semibold whitespace-nowrap';

/** How bad it is, said in words as well as colour. */
export function IncidentSeverityBadge({ severity }: { readonly severity: Severity }) {
  return <span className={`${PILL} ${severityToneClass(severity)}`}>{severityLabel(severity)}</span>;
}

/** Where the room stands in its lifecycle. */
export function IncidentStatusBadge({ status }: { readonly status: Status }) {
  return <span className={`${PILL} ${statusToneClass(status)}`}>{statusLabel(status)}</span>;
}
