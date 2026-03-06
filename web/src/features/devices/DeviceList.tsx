import { useEffect } from 'react';
import { useDeviceStore } from '../../state/device-store';
import { GroupSidebar } from './GroupSidebar';
import { DeviceCard } from './DeviceCard';

export function DeviceList() {
  const devices = useDeviceStore((s) => s.devices);
  const selectedGroupId = useDeviceStore((s) => s.selectedGroupId);
  const isLoading = useDeviceStore((s) => s.isLoading);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);

  useEffect(() => {
    fetchGroups();
  }, [fetchGroups]);

  return (
    <div className="flex h-[calc(100vh-57px)]">
      <GroupSidebar />
      <div className="flex-1 p-6">
        {!selectedGroupId && (
          <p className="text-gray-500">Select a group to view devices</p>
        )}

        {selectedGroupId && isLoading && (
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

        {selectedGroupId && !isLoading && devices.length === 0 && (
          <p className="text-gray-500">No devices in this group</p>
        )}

        {selectedGroupId && !isLoading && devices.length > 0 && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {devices.map((device) => (
              <DeviceCard key={device.id} device={device} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
