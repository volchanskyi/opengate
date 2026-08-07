import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { Link, useSearchParams } from 'react-router';
import { useVirtualizer } from '@tanstack/react-virtual';
import { applyDeviceFilter, parseDeviceFilter, describeDeviceFilter } from './device-filter';
import { useDeviceStore } from './state/device-store';
import { useUpdateStore } from './state/update-store';
import { useInventoryStore } from './state/inventory-store';
import { useToastStore } from '../../lib/feedback/toast-store';
import { SiteSidebar } from './SiteSidebar';
import { DeviceCard } from './DeviceCard';
import { DeviceSearchBar } from './DeviceSearchBar';
import { fireAndForget } from '../../lib/fire-and-forget';
import { useVisibleInterval } from '../../lib/use-visible-interval';
import { useOrganizationStore } from '../organizations';

/** How often the grid refreshes device status while the tab is visible. */
const DEVICE_LIST_POLL_MS = 15_000;

// Estimated rendered height of one DeviceCard row including the grid gap. Exact
// precision is not required — the virtualizer only uses it to place rows; cards
// have a stable size so a fixed estimate avoids per-row DOM measurement.
const DEVICE_ROW_HEIGHT = 132;

// Card columns per row, mirroring the responsive Tailwind grid this replaced
// (grid-cols-1 / md:grid-cols-2 / lg:grid-cols-3). Virtualization needs the
// column count in JS to map a flat device list onto virtual rows.
const COLUMN_BREAKPOINTS = [
  { minWidth: 1024, columns: 3 },
  { minWidth: 768, columns: 2 },
] as const;

function useColumnCount(ref: RefObject<HTMLElement | null>): number {
  const [columns, setColumns] = useState(1);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => {
      const width = el.getBoundingClientRect().width;
      const match = COLUMN_BREAKPOINTS.find((b) => width >= b.minWidth);
      setColumns(match ? match.columns : 1);
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => { observer.disconnect(); };
  }, [ref]);
  return columns;
}

export function DeviceList() {
  const devices = useDeviceStore((s) => s.devices);
  const selectedSiteId = useDeviceStore((s) => s.selectedSiteId);
  const isLoading = useDeviceStore((s) => s.isLoading);
  const fetchSites = useDeviceStore((s) => s.fetchSites);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  // The picked customer is a narrowing of this list, so a change to it re-reads
  // exactly like a change to the site filter does.
  const selectedOrganizationId = useOrganizationStore((s) => s.selectedOrganizationId);
  const upgradeAgent = useDeviceStore((s) => s.upgradeAgent);
  const manifests = useUpdateStore((s) => s.manifests);
  const fetchManifests = useUpdateStore((s) => s.fetchManifests);
  const addToast = useToastStore((s) => s.addToast);
  const [searchQuery, setSearchQuery] = useState('');
  const [isUpgradingAll, setIsUpgradingAll] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const deviceFilter = useMemo(() => parseDeviceFilter(searchParams), [searchParams]);
  const filterLabel = describeDeviceFilter(deviceFilter);
  const clearFilter = useCallback(() => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete('status');
      next.delete('maintenance');
      next.delete('health');
      return next;
    });
  }, [setSearchParams]);

  useEffect(() => {
    fireAndForget(fetchSites());
    fireAndForget(fetchDevices());
    fireAndForget(fetchManifests());
  }, [fetchSites, fetchDevices, fetchManifests, selectedOrganizationId]);

  // Poll device status so online/offline stays current. A hidden tab issues
  // nothing and catches up the moment it is shown again.
  useVisibleInterval(() => {
    fireAndForget(fetchDevices(selectedSiteId ?? undefined));
  }, DEVICE_LIST_POLL_MS);

  const scrollParentRef = useRef<HTMLDivElement>(null);
  const columns = useColumnCount(scrollParentRef);

  const handleSearch = useCallback((q: string) => setSearchQuery(q), []);

  const filteredDevices = useMemo(() => {
    const narrowed = applyDeviceFilter(devices, deviceFilter);
    if (!searchQuery) return narrowed;
    const q = searchQuery.toLowerCase();
    return narrowed.filter(
      (d) =>
        d.hostname.toLowerCase().includes(q) ||
        d.os.toLowerCase().includes(q),
    );
  }, [devices, searchQuery, deviceFilter]);

  // Devices that have an available upgrade (version behind latest manifest for their OS).
  const outdatedDevices = useMemo(() => {
    return devices.filter((d) => {
      const latest = manifests
        .filter((m) => m.os === d.os)
        .sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))[0];
      return (
        latest &&
        d.agent_version &&
        d.agent_version.localeCompare(latest.version, undefined, { numeric: true }) < 0 &&
        d.status === 'online'
      );
    });
  }, [devices, manifests]);

  const handleUpgradeAll = useCallback(async () => {
    if (outdatedDevices.length === 0) return;
    setIsUpgradingAll(true);
    let succeeded = 0;
    let failed = 0;
    for (const d of outdatedDevices) {
      const latest = manifests
        .filter((m) => m.os === d.os)
        .sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))[0];
      if (!latest) continue;
      const ok = await upgradeAgent(d.id, latest.version, latest.os, latest.arch);
      if (ok) succeeded++;
      else failed++;
    }
    if (failed === 0) {
      addToast(`Upgrade pushed to ${succeeded} device${succeeded !== 1 ? 's' : ''}`, 'success');
    } else {
      addToast(`Upgraded ${succeeded}, failed ${failed}`, 'error');
    }
    setIsUpgradingAll(false);
  }, [outdatedDevices, manifests, upgradeAgent, addToast]);

  const rowCount = Math.ceil(filteredDevices.length / columns);
  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => scrollParentRef.current,
    estimateSize: () => DEVICE_ROW_HEIGHT,
    overscan: 4,
  });

  // Lazily load the discovered-footprint hint for the devices actually mounted.
  // Virtualization bounds this to the visible window; the store is cache-first so
  // re-mounts during scroll are cheap no-ops. Keyed on the id set (UUIDs, so a
  // comma join is unambiguous) to fire only when the visible devices change.
  const fetchInventory = useInventoryStore((s) => s.fetchInventory);
  const virtualRows = rowVirtualizer.getVirtualItems();
  const visibleIds = useMemo(() => {
    const ids: string[] = [];
    for (const vr of virtualRows) {
      const start = vr.index * columns;
      for (const d of filteredDevices.slice(start, start + columns)) ids.push(d.id);
    }
    return ids.join(',');
  }, [virtualRows, columns, filteredDevices]);
  useEffect(() => {
    for (const id of visibleIds ? visibleIds.split(',') : []) {
      fireAndForget(fetchInventory(id));
    }
  }, [visibleIds, fetchInventory]);

  return (
    <div className="flex h-[calc(100vh-57px)]">
      <SiteSidebar />
      <div className="flex-1 p-6 flex flex-col gap-4 min-h-0">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <DeviceSearchBar
              onSearch={handleSearch}
              totalCount={devices.length}
              filteredCount={filteredDevices.length}
            />
            {filterLabel && (
              <button
                type="button"
                onClick={clearFilter}
                aria-label={`Clear filter: ${filterLabel}`}
                title="Clear filter"
                className="px-2 py-0.5 rounded text-xs bg-blue-900/60 text-blue-200 hover:bg-blue-900 whitespace-nowrap"
              >
                {filterLabel} ✕
              </button>
            )}
          </div>
          <div className="flex gap-2">
            {outdatedDevices.length > 0 && (
              <button
                type="button"
                onClick={() => { fireAndForget(handleUpgradeAll()); }}
                disabled={isUpgradingAll}
                className="px-3 py-2 bg-green-600 hover:bg-green-700 rounded text-sm whitespace-nowrap disabled:opacity-50"
              >
                {isUpgradingAll
                  ? 'Upgrading...'
                  : `Upgrade All Agents (${outdatedDevices.length})`}
              </button>
            )}
            <Link to="/setup" className="px-3 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm whitespace-nowrap">
              Add Device
            </Link>
          </div>
        </div>

        <div ref={scrollParentRef} className="flex-1 overflow-auto min-h-0">
          {isLoading && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {[1, 2, 3].map((i) => (
                <div key={i} className="bg-gray-800 border border-gray-700 rounded-lg p-4 animate-pulse">
                  <div className="h-5 bg-gray-700 rounded w-1/2 mb-3" />
                  <div className="h-4 bg-gray-700 rounded w-3/4 mb-2" />
                  <div className="h-4 bg-gray-700 rounded w-1/3" />
                </div>
              ))}
            </div>
          )}

          {!isLoading && filteredDevices.length === 0 && (
            <div className="text-center py-12">
              <h3 className="text-lg font-semibold mb-2">
                {searchQuery
                  ? 'No devices match your search'
                  : filterLabel
                    ? `No devices match the "${filterLabel}" filter`
                    : selectedSiteId
                      ? 'No devices in this site'
                      : 'Welcome to OpenGate'}
              </h3>
              <p className="text-gray-500 mb-4">
                {searchQuery
                  ? 'Try a different search term.'
                  : filterLabel
                    ? 'Clear the filter to see all devices.'
                    : selectedSiteId
                      ? 'Download and install the agent to add devices.'
                      : 'Select a site to filter devices, or add a new device to get started.'}
              </p>
            </div>
          )}

          {/* Virtualized device grid: only rows in (or near) the viewport are
              mounted, so the page stays responsive regardless of device count.
              filteredDevices is mapped onto virtual rows of `columns` cards. */}
          {!isLoading && filteredDevices.length > 0 && (
            <div style={{ height: rowVirtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
              {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                const start = virtualRow.index * columns;
                const rowDevices = filteredDevices.slice(start, start + columns);
                return (
                  <div
                    key={virtualRow.key}
                    className="grid gap-4"
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      transform: `translateY(${String(virtualRow.start)}px)`,
                      gridTemplateColumns: `repeat(${String(columns)}, minmax(0, 1fr))`,
                    }}
                  >
                    {rowDevices.map((device) => (
                      <DeviceCard key={device.id} device={device} />
                    ))}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
