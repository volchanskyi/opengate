import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { HostSelect, type HostOption } from './HostSelect';

const HOSTS: readonly HostOption[] = [
  { id: 'h1', name: 'backup-01', online: true },
  { id: 'h2', name: 'reception-pc', online: false },
  { id: 'h3', name: 'reception-pc', online: true },
  { id: 'h4', name: 'Rack-switch', online: true },
];

function show(over: Partial<React.ComponentProps<typeof HostSelect>> = {}) {
  const onChange = vi.fn();
  render(
    <HostSelect label="Host" hosts={HOSTS} value="" onChange={onChange} emptyLabel="All hosts" {...over} />,
  );
  return onChange;
}

function option(list: HTMLElement, index: number): HTMLElement {
  const found = within(list).getAllByRole('option').at(index);
  if (!found) throw new Error(`no option at ${String(index)}`);
  return found;
}

function openList() {
  fireEvent.click(screen.getByRole('button', { name: /Host/ }));
  return screen.getByRole('listbox', { name: 'Host' });
}

describe('HostSelect', () => {
  it('names the current choice on the closed control', () => {
    show({ value: 'h4' });
    expect(screen.getByRole('button', { name: /Host/ })).toHaveTextContent('Rack-switch');
  });

  it('offers the empty choice first, then every host in the order given', () => {
    show();
    const options = within(openList()).getAllByRole('option');
    expect(options.map((o) => o.textContent)).toEqual([
      'All hosts', 'backup-01online', 'reception-pcoffline', 'reception-pconline', 'Rack-switchonline',
    ]);
  });

  it('marks each host green when online and grey when offline, and says which in words', () => {
    show();
    const list = openList();
    const offline = option(list, 2);
    expect(offline.querySelector('[aria-hidden="true"]')).toHaveClass('bg-gray-500');
    expect(within(offline).getByText('offline')).toHaveClass('sr-only');
    const online = option(list, 1);
    expect(online.querySelector('[aria-hidden="true"]')).toHaveClass('bg-green-500');
  });

  it('keeps two hosts of one name apart, so either can be picked', () => {
    const onChange = show();
    fireEvent.click(option(openList(), 3));
    expect(onChange).toHaveBeenCalledWith('h3');
  });

  it('picks a host when its name, inside the row, is clicked', () => {
    const onChange = show();
    fireEvent.click(within(option(openList(), 1)).getByText('backup-01'));
    expect(onChange).toHaveBeenCalledWith('h1');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('picks nothing and stays open when the list is clicked between rows', () => {
    const onChange = show();
    const list = openList();
    fireEvent.click(list);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole('listbox', { name: 'Host' })).toBe(list);
  });

  it('walks the list with the arrows and picks with Enter', () => {
    const onChange = show();
    const list = openList();
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: 'ArrowUp' });
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('h1');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('jumps to the ends with Home and End', () => {
    const onChange = show();
    const list = openList();
    fireEvent.keyDown(list, { key: 'End' });
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onChange).toHaveBeenLastCalledWith('h4');

    const again = openList();
    fireEvent.keyDown(again, { key: 'Home' });
    fireEvent.keyDown(again, { key: 'Enter' });
    expect(onChange).toHaveBeenLastCalledWith('');
  });

  it('jumps to the next host starting with a typed letter, whatever its case', () => {
    const onChange = show();
    const list = openList();
    fireEvent.keyDown(list, { key: 'r' });
    fireEvent.keyDown(list, { key: 'r' });
    fireEvent.keyDown(list, { key: 'r' });
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('h4');
  });

  it('closes on Escape without changing the choice', () => {
    const onChange = show();
    const list = openList();
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: 'Escape' });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('opens from the keyboard on the closed control', () => {
    show();
    fireEvent.keyDown(screen.getByRole('button', { name: /Host/ }), { key: 'ArrowDown' });
    expect(screen.getByRole('listbox', { name: 'Host' })).toBeInTheDocument();
  });

  it('marks the current choice as selected in the open list', () => {
    show({ value: 'h2' });
    const selected = within(openList()).getAllByRole('option').filter((o) => o.getAttribute('aria-selected') === 'true');
    expect(selected.map((o) => o.textContent)).toEqual(['reception-pcoffline']);
  });

  it('leaves out the empty choice where a host must be picked', () => {
    show({ emptyLabel: 'Select a host', allowEmpty: false });
    expect(screen.getByRole('button', { name: /Host/ })).toHaveTextContent('Select a host');
    expect(option(openList(), 0)).toHaveTextContent('backup-01');
  });

  it('stays shut and says why while it is waiting on a customer', () => {
    show({ disabled: true, disabledLabel: 'Pick a customer first' });
    const control = screen.getByRole('button', { name: /Host/ });
    expect(control).toBeDisabled();
    expect(control).toHaveTextContent('Pick a customer first');
    fireEvent.click(control);
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('closes when focus leaves the open list', () => {
    show();
    const list = openList();
    fireEvent.blur(list, { relatedTarget: document.body });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('closes again on a second press of the control', () => {
    show();
    openList();
    fireEvent.click(screen.getByRole('button', { name: /Host/ }));
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('follows the pointer, so Enter picks what the pointer is over', () => {
    const onChange = show();
    const list = openList();
    fireEvent.mouseEnter(option(list, 2));
    expect(list).toHaveAttribute('aria-activedescendant', option(list, 2).id);
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('h2');
  });

  it('opens on nothing when there is nothing to pick', () => {
    show({ hosts: [], allowEmpty: false });
    fireEvent.click(screen.getByRole('button', { name: /Host/ }));
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });
});
