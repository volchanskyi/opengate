import { Link, useLocation, useParams } from 'react-router';
import { shortId } from '../lib/short-id';
import { routeCrumbOf } from '../lib/use-route-crumb';

interface Crumb {
  label: string;
  to?: string;
}

/** How a fixed path segment is labelled, as the last crumb (`lastLabel`) or as a link (`link`). */
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
  const named = routeCrumbOf(location.state);
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

    // An id takes the label its page handed the route, until then what its section calls it.
    const under = crumbs.at(-1)?.label;
    if (under === 'Devices') {
      const label = named ?? seg;
      crumbs.push(isLast ? { label } : { label, to: path });
    } else if (under === 'Investigations') {
      crumbs.push({ label: named ?? shortId(seg) });
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
