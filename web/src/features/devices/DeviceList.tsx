import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { GroupSidebar } from './GroupSidebar';
import { DeviceCard } from './DeviceCard';
import { DeviceSearchBar } from './DeviceSearchBar';

export function DeviceList() {
  const devices = useDeviceStore((s) => s.devices);
  const selectedGroupId = useDeviceStore((s) => s.selectedGroupId);
  const isLoading = useDeviceStore((s) => s.isLoading);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    fetchGroups();
    fetchDevices();
  }, [fetchGroups, fetchDevices]);

  // Poll device status so online/offline stays current.
  useEffect(() => {
    const interval = setInterval(() => {
      fetchDevices(selectedGroupId ?? undefined);
    }, 15_000);
    return () => clearInterval(interval);
  }, [fetchDevices, selectedGroupId]);

  const handleSearch = useCallback((q: string) => setSearchQuery(q), []);

  const filteredDevices = useMemo(() => {
    if (!searchQuery) return devices;
    const q = searchQuery.toLowerCase();
    return devices.filter(
      (d) =>
        d.hostname.toLowerCase().includes(q) ||
        d.os.toLowerCase().includes(q),
    );
  }, [devices, searchQuery]);

  return (
    <div className="flex h-[calc(100vh-57px)]">
      <GroupSidebar />
      <div className="flex-1 p-6 space-y-4">
        <div className="flex items-center justify-between">
          <DeviceSearchBar
            onSearch={handleSearch}
            totalCount={devices.length}
            filteredCount={filteredDevices.length}
          />
          <Link to="/setup" className="px-3 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm whitespace-nowrap">
            Add Device
          </Link>
        </div>

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
                : selectedGroupId
                  ? 'No devices in this group'
                  : 'Welcome to OpenGate'}
            </h3>
            <p className="text-gray-500 mb-4">
              {searchQuery
                ? 'Try a different search term.'
                : selectedGroupId
                  ? 'Download and install the agent to add devices.'
                  : 'Select a group to filter devices, or add a new device to get started.'}
            </p>
          </div>
        )}

        {!isLoading && filteredDevices.length > 0 && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {filteredDevices.map((device) => (
              <DeviceCard key={device.id} device={device} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
