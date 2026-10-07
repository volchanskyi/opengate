import { useEffect, useId, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react';

/** One host a pick-list offers: its name and whether it is connected now. */
export interface HostOption {
  readonly id: string;
  readonly name: string;
  readonly online: boolean;
}

/** A green (online) or grey (offline) dot, hidden from assistive technology. */
function StatusDot({ online }: { readonly online: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block h-2 w-2 shrink-0 rounded-full ${online ? 'bg-green-500' : 'bg-gray-500'}`}
    />
  );
}

/** A host's name after its status dot, with the state also in words. */
export function HostLabel({ name, online }: { readonly name: string; readonly online: boolean }) {
  return (
    <>
      <StatusDot online={online} />
      <span className="truncate">{name}</span>
      <span className="sr-only">{online ? 'online' : 'offline'}</span>
    </>
  );
}

interface Props {
  /** The control's name, read with the current choice. */
  readonly label: string;
  readonly hosts: readonly HostOption[];
  /** The chosen host id; empty for the empty choice. */
  readonly value: string;
  readonly onChange: (id: string) => void;
  /** What the empty choice reads, such as "All hosts" or "Select a host". */
  readonly emptyLabel: string;
  /** Whether the empty choice is offered as the first option. */
  readonly allowEmpty?: boolean;
  readonly disabled?: boolean;
  /** What the shut control reads while disabled. */
  readonly disabledLabel?: string;
}

interface Entry {
  readonly id: string;
  readonly name: string;
  readonly online: boolean | null;
}

/** The index of the next entry after `from` whose name starts with `letter`, wrapping round. */
function nextStartingWith(entries: readonly Entry[], from: number, letter: string): number {
  const wanted = letter.toLowerCase();
  for (let step = 1; step <= entries.length; step++) {
    const index = (from + step) % entries.length;
    if (entries.at(index)?.name.toLowerCase().startsWith(wanted)) return index;
  }
  return from;
}

/**
 * A host drop-down: a native select cannot colour a dot, so this is a listbox driven by arrows,
 * Home and End, Enter, Escape and a first-letter jump.
 */
export function HostSelect({
  label, hosts, value, onChange, emptyLabel, allowEmpty = true, disabled = false, disabledLabel,
}: Props) {
  const baseId = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const entries: readonly Entry[] = [
    ...(allowEmpty ? [{ id: '', name: emptyLabel, online: null }] : []),
    ...hosts.map((h) => ({ id: h.id, name: h.name, online: h.online })),
  ];
  const chosen = entries.find((e) => e.id === value);
  const shown = disabled ? disabledLabel ?? emptyLabel : chosen?.name ?? emptyLabel;
  const chosenOnline = disabled ? null : (chosen?.online ?? null);

  useEffect(() => {
    if (open) listRef.current?.focus();
  }, [open]);

  const openList = () => {
    if (disabled || entries.length === 0) return;
    setActive(Math.max(0, entries.findIndex((e) => e.id === value)));
    setOpen(true);
  };

  const close = () => {
    setOpen(false);
    buttonRef.current?.focus();
  };

  const pick = (index: number) => {
    const entry = entries.at(index);
    if (entry) onChange(entry.id);
    close();
  };

  const onListKey = (e: KeyboardEvent<HTMLUListElement>) => {
    const last = entries.length - 1;
    switch (e.key) {
      case 'ArrowDown': setActive((i) => Math.min(last, i + 1)); break;
      case 'ArrowUp': setActive((i) => Math.max(0, i - 1)); break;
      case 'Home': setActive(0); break;
      case 'End': setActive(last); break;
      case 'Enter': pick(active); break;
      case 'Escape': close(); break;
      default:
        if (e.key.length !== 1 || e.key.trim() === '') return;
        setActive((i) => nextStartingWith(entries, i, e.key));
    }
    e.preventDefault();
  };

  /** Focus stays on the list, so the list takes the click and picks the row it landed in. */
  const onListClick = (e: MouseEvent<HTMLUListElement>) => {
    const row = e.target instanceof Element ? e.target.closest('[data-index]') : null;
    if (row instanceof HTMLElement) pick(Number(row.dataset.index));
  };

  const optionId = (index: number) => `${baseId}-option-${String(index)}`;

  return (
    <div className="relative">
      <span id={`${baseId}-label`} className="sr-only">{label}</span>
      <button
        ref={buttonRef}
        type="button"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-labelledby={`${baseId}-label ${baseId}-value`}
        onClick={() => { if (open) close(); else openList(); }}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
          e.preventDefault();
          openList();
        }}
        className="flex w-56 items-center gap-2 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-gray-100 disabled:opacity-60 disabled:cursor-not-allowed"
      >
        {chosenOnline !== null && <StatusDot online={chosenOnline} />}
        <span id={`${baseId}-value`} className="truncate">{shown}</span>
        <span aria-hidden="true" className="ml-auto text-xs text-gray-500">&#9662;</span>
      </button>

      {open && (
        <ul
          ref={listRef}
          role="listbox"
          aria-label={label}
          tabIndex={-1}
          aria-activedescendant={optionId(active)}
          onKeyDown={onListKey}
          onClick={onListClick}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false);
          }}
          className="absolute z-20 mt-1 max-h-64 w-56 overflow-auto rounded border border-gray-600 bg-gray-900 py-1 shadow-lg focus:outline-none"
        >
          {entries.map((entry, index) => (
            <li
              key={entry.id || 'empty'}
              id={optionId(index)}
              role="option"
              aria-selected={entry.id === value}
              data-index={index}
              onMouseEnter={() => { setActive(index); }}
              className={`flex cursor-pointer items-center gap-2 px-2 py-1 text-sm ${
                index === active ? 'bg-blue-700 text-white' : 'text-gray-200'
              }`}
            >
              {entry.online === null
                ? <span className="truncate">{entry.name}</span>
                : <HostLabel name={entry.name} online={entry.online} />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
