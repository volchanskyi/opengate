import { Link, useLocation, useParams } from 'react-router';
import { useDeviceStore } from '../features/devices/state/device-store';
import { shortId } from '../lib/short-id';

interface Crumb {
  label: string;
  to?: string;
}

/**
 * How each fixed path segment names itself.
 *
 * `lastLabel` is what the segment is called when it is where the reader
 * currently stands. `link` is where it points when it is not: a literal path for
 * a section with one home, `'path'` for one that lives wherever it was reached
 * from, and absent for a segment that is never a link.
 */
interface SegmentRule {
  readonly label: string;
  readonly lastLabel?: string;
  readonly link?: string;
}

const SELF = 'path';

const SEGMENTS = new Map<string, SegmentRule>([
  ['devices', { label: 'Devices', link: '/devices' }],
  ['investigations', { label: 'Investigations', link: '/investigations' }],
  ['sessions', { label: 'Sessions', lastLabel: 'Session', link: SELF }],
  ['settings', { label: 'Settings', link: '/settings' }],
  ['users', { label: 'Users', link: SELF }],
  ['audit', { label: 'Audit Log', link: SELF }],
  ['updates', { label: 'Agent Settings', link: SELF }],
  ['permissions', { label: 'Permissions' }],
  ['setup', { label: 'Add Device' }],
  ['profile', { label: 'Profile' }],
]);

function fixedCrumb(rule: SegmentRule, path: string, isLast: boolean): Crumb {
  const label = isLast ? rule.lastLabel ?? rule.label : rule.label;
  if (isLast || rule.link === undefined) return { label };
  return { label, to: rule.link === SELF ? path : rule.link };
}

export function Breadcrumbs() {
  const location = useLocation();
  const params = useParams();
  const device = useDeviceStore((s) => s.selectedDevice);
  const segments = location.pathname.split('/').filter(Boolean);

  if (segments.length === 0) return null;

  const crumbs: Crumb[] = [];
  let path = '';

  segments.forEach((seg, i, arr) => {
    path += `/${seg}`;
    const isLast = i === arr.length - 1;

    const rule = SEGMENTS.get(seg);
    if (rule) {
      crumbs.push(fixedCrumb(rule, path, isLast));
      return;
    }
    if (seg === params.token) {
      crumbs.push({ label: 'Session' });
      return;
    }
    if (seg !== params.id) return;

    // An id names itself by whatever the section it sits under calls it.
    const under = crumbs.at(-1)?.label;
    if (under === 'Devices') {
      const label = device?.hostname ?? seg;
      crumbs.push(isLast ? { label } : { label, to: path });
    } else if (under === 'Investigations') {
      crumbs.push({ label: shortId(seg) });
    }
  });

  if (crumbs.length === 0) return null;

  return (
    <nav className="px-6 py-2 text-sm text-gray-400 flex items-center gap-1">
      <Link to="/" className="hover:text-white">Dashboard</Link>
      {crumbs.map((crumb) => (
        <span key={`${crumb.label}-${crumb.to ?? ''}`} className="flex items-center gap-1">
          <span className="mx-1">&gt;</span>
          {crumb.to ? (
            <Link to={crumb.to} className="hover:text-white">{crumb.label}</Link>
          ) : (
            <span className="text-white">{crumb.label}</span>
          )}
        </span>
      ))}
    </nav>
  );
}
