import { useEffect, useMemo, useState } from 'react';
import { useInventoryStore } from './state/inventory-store';
import type { components } from '../../types/api';
import { fireAndForget } from '../../lib/fire-and-forget';

type InventoryItem = components['schemas']['InventoryItem'];
type Kind = InventoryItem['kind'];

interface Column {
  readonly label: string;
  readonly get: (it: InventoryItem) => string | number;
  readonly numeric?: boolean;
  readonly date?: boolean;
}

interface KindMeta {
  readonly label: string;
  readonly columns: readonly Column[];
}

const dash = (v: string) => (v === '' ? '—' : v);
const lastSeen: Column = { label: 'Last seen', get: (it) => it.last_seen, date: true };

// Display order + per-kind table shape. Column accessors avoid dynamic property
// indexing, and only the columns meaningful to a kind are shown, so a ports
// table reads differently from a packages table.
const KIND_ORDER: readonly Kind[] = ['port', 'service', 'db_engine', 'container', 'package'];

const KIND_META = new Map<Kind, KindMeta>([
  ['port', { label: 'Listening Ports', columns: [
    { label: 'Port', get: (it) => it.port, numeric: true },
    { label: 'Proto', get: (it) => dash(it.proto) },
    { label: 'State', get: (it) => dash(it.state) },
    { label: 'Process', get: (it) => dash(it.name) },
    lastSeen,
  ] }],
  ['service', { label: 'Services', columns: [
    { label: 'Unit', get: (it) => it.name },
    { label: 'State', get: (it) => dash(it.state) },
    { label: 'Version', get: (it) => dash(it.version) },
    lastSeen,
  ] }],
  ['db_engine', { label: 'Database Engines', columns: [
    { label: 'Engine', get: (it) => it.name },
    { label: 'Version', get: (it) => dash(it.version) },
    { label: 'Port', get: (it) => it.port, numeric: true },
    lastSeen,
  ] }],
  ['container', { label: 'Containers', columns: [
    { label: 'Name', get: (it) => it.name },
    { label: 'Image', get: (it) => dash(it.image) },
    { label: 'Runtime', get: (it) => dash(it.runtime) },
    { label: 'State', get: (it) => dash(it.state) },
    lastSeen,
  ] }],
  ['package', { label: 'Packages', columns: [
    { label: 'Name', get: (it) => it.name },
    { label: 'Version', get: (it) => dash(it.version) },
    lastSeen,
  ] }],
]);

function cellText(it: InventoryItem, col: Column): string {
  if (col.date) {
    const v = col.get(it);
    return v ? new Date(String(v)).toLocaleString() : '—';
  }
  return String(col.get(it));
}

function compareItems(a: InventoryItem, b: InventoryItem, col: Column): number {
  if (col.numeric) return (col.get(a) as number) - (col.get(b) as number);
  if (col.date) return new Date(String(col.get(a))).getTime() - new Date(String(col.get(b))).getTime();
  return String(col.get(a)).localeCompare(String(col.get(b)));
}

function InventoryTable({ meta, items }: { readonly meta: KindMeta; readonly items: readonly InventoryItem[] }) {
  const { label, columns } = meta;
  const firstCol = columns.at(0);
  const [sortLabel, setSortLabel] = useState(firstCol?.label ?? '');
  const [asc, setAsc] = useState(true);

  const sortCol = columns.find((c) => c.label === sortLabel) ?? firstCol;
  const sorted = useMemo(() => {
    const copy = [...items];
    if (sortCol) copy.sort((a, b) => compareItems(a, b, sortCol));
    if (!asc) copy.reverse();
    return copy;
  }, [items, sortCol, asc]);

  const toggle = (colLabel: string) => {
    if (colLabel === sortLabel) {
      setAsc((v) => !v);
    } else {
      setSortLabel(colLabel);
      setAsc(true);
    }
  };

  return (
    <div className="mb-4">
      <h4 className="text-xs font-semibold text-gray-400 mb-1">{label} ({items.length})</h4>
      <div className="overflow-x-auto bg-gray-900 border border-gray-700 rounded">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-left text-gray-500">
              {columns.map((col) => (
                <th key={col.label} className="px-2 py-1 font-semibold">
                  <button type="button" onClick={() => toggle(col.label)} className="hover:text-gray-300">
                    {col.label}{sortLabel === col.label ? (asc ? ' ▲' : ' ▼') : ''}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((it) => (
              <tr key={`${it.kind}-${it.name}-${String(it.port)}`} className="border-t border-gray-800 hover:bg-gray-800">
                {columns.map((col) => (
                  <td key={col.label} className="px-2 py-1 text-gray-300 whitespace-nowrap">{cellText(it, col)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function DeviceInventory({ deviceId }: { readonly deviceId: string }) {
  const items = useInventoryStore((s) => s.byDevice.get(deviceId));
  const loading = useInventoryStore((s) => s.loading.get(deviceId));
  const error = useInventoryStore((s) => s.errors.get(deviceId));
  const fetchInventory = useInventoryStore((s) => s.fetchInventory);

  useEffect(() => {
    fireAndForget(fetchInventory(deviceId));
  }, [deviceId, fetchInventory]);

  const grouped = useMemo(() => {
    const g = new Map<Kind, InventoryItem[]>();
    for (const it of items ?? []) {
      const arr = g.get(it.kind) ?? [];
      arr.push(it);
      g.set(it.kind, arr);
    }
    return g;
  }, [items]);

  const header = (
    <div className="flex items-center justify-between mb-3">
      <h3 className="text-sm font-semibold text-gray-300">Discovered Footprint</h3>
      <button
        type="button"
        onClick={() => { fireAndForget(fetchInventory(deviceId, true)); }}
        disabled={loading}
        className="px-3 py-1 bg-blue-600 hover:bg-blue-500 rounded text-xs font-medium disabled:opacity-50"
      >
        {loading ? 'Refreshing...' : 'Refresh'}
      </button>
    </div>
  );

  // Not yet loaded: distinguish an in-flight fetch from a failed one.
  if (items === undefined) {
    return (
      <div>
        {header}
        {error
          ? <p className="text-xs text-red-400">{error}</p>
          : <p className="text-xs text-gray-500">Loading inventory…</p>}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div>
        {header}
        <p className="text-xs text-gray-500">
          No footprint discovered yet — the agent reports ports, services, and containers after its first scan.
        </p>
      </div>
    );
  }

  const kinds = KIND_ORDER.filter((k) => grouped.has(k));
  // Freshly-enrolled summary: at-a-glance counts so a new host is instantly legible.
  const summary = kinds
    .map((k) => `${String(grouped.get(k)?.length ?? 0)} ${(KIND_META.get(k)?.label ?? k).toLowerCase()}`)
    .join(' · ');

  return (
    <div>
      {header}
      <p className="text-xs text-gray-400 mb-3">Discovered: {summary}</p>
      {kinds.map((k) => {
        const meta = KIND_META.get(k);
        return meta ? <InventoryTable key={k} meta={meta} items={grouped.get(k) ?? []} /> : null;
      })}
    </div>
  );
}
