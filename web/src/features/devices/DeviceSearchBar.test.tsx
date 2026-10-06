import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { DeviceSearchBar } from './DeviceSearchBar';

async function settleDebounce() {
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
}

function typeQuery(text: string) {
  const input = screen.getByPlaceholderText<HTMLInputElement>('Search Devices...');
  for (let end = 1; end <= text.length; end += 1) {
    fireEvent.change(input, { target: { value: text.slice(0, end) } });
  }
  return input;
}

describe('DeviceSearchBar', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('searches once for a burst of keystrokes, with the final query', async () => {
    const onSearch = vi.fn();
    render(<DeviceSearchBar onSearch={onSearch} totalCount={9} filteredCount={9} />);
    await settleDebounce();
    onSearch.mockClear(); // the mount fires one search for the empty query

    typeQuery('web');
    await settleDebounce();

    expect(onSearch).toHaveBeenCalledTimes(1);
    expect(onSearch).toHaveBeenCalledWith('web');
  });

  it('offers no clear control and no match count until something is typed', () => {
    render(<DeviceSearchBar onSearch={vi.fn()} totalCount={9} filteredCount={9} />);

    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByText(/9 of 9/)).toBeNull();
  });

  it('shows how much of the fleet the query matched', () => {
    render(<DeviceSearchBar onSearch={vi.fn()} totalCount={9} filteredCount={2} />);

    typeQuery('web');

    expect(screen.getByText(/2 of 9/)).toBeInTheDocument();
  });

  it('clearing the query restores the unfiltered fleet', async () => {
    const onSearch = vi.fn();
    render(<DeviceSearchBar onSearch={onSearch} totalCount={9} filteredCount={2} />);
    const input = typeQuery('web');
    await settleDebounce();
    onSearch.mockClear();

    fireEvent.click(screen.getByRole('button'));
    await settleDebounce();

    expect(input.value).toBe('');
    expect(onSearch).toHaveBeenCalledWith('');
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByText(/of 9/)).toBeNull();
  });
});
