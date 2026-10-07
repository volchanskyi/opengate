import { useEffect } from 'react';

interface Props {
  /** The typed text, owned by the list so it can clear it. */
  query: string;
  onQueryChange: (query: string) => void;
  /** Called with the text once typing pauses. */
  onSearch: (query: string) => void;
  totalCount: number;
  filteredCount: number;
}

export function DeviceSearchBar({ query, onQueryChange, onSearch, totalCount, filteredCount }: Props) {
  useEffect(() => {
    const timer = setTimeout(() => onSearch(query), 300);
    return () => clearTimeout(timer);
  }, [query, onSearch]);

  return (
    <div className="flex items-center gap-3">
      <div className="relative flex-1 max-w-md">
        <input
          type="text"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="Search Devices..."
          className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm pr-8"
        />
        {query && (
          <button
            type="button"
            onClick={() => onQueryChange('')}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-white text-xs"
          >
            x
          </button>
        )}
      </div>
      {query && (
        <span className="text-xs text-gray-400">
          {filteredCount} of {totalCount}
        </span>
      )}
    </div>
  );
}
