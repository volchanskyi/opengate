import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useDeviceStore } from '../../state/device-store';
import { GroupSidebar } from './GroupSidebar';
import { DeviceCard } from './DeviceCard';

export function DeviceList() {
  const devices = useDeviceStore((s) => s.devices);
  const selectedGroupId = useDeviceStore((s) => s.selectedGroupId);
  const isLoading = useDeviceStore((s) => s.isLoading);
  const fetchGroups = useDeviceStore((s) => s.fetchGroups);
  const fetchDevices = useDeviceStore((s) => s.fetchDevices);

  useEffect(() => {
    fetchGroups();
    fetchDevices();
  }, [fetchGroups, fetchDevices]);

  return (
    <div className="flex h-[calc(100vh-57px)]">
      <GroupSidebar />
      <div className="flex-1 p-6">
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

        {!isLoading && devices.length === 0 && (
          <div className="text-center py-12">
            <h3 className="text-lg font-semibold mb-2">
              {selectedGroupId ? 'No devices in this group' : 'Welcome to OpenGate'}
            </h3>
            <p className="text-gray-500 mb-4">
              {selectedGroupId
                ? 'Download and install the agent to add devices.'
                : 'Select a group to filter devices, or add a new device to get started.'}
            </p>
            <Link to="/setup" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded text-sm">
              Add Device
            </Link>
          </div>
        )}

        {!isLoading && devices.length > 0 && (
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
