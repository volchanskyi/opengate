import type { SVGProps } from 'react';

/**
 * Small inline stroke icons (24×24, `currentColor`) used inside icon-only
 * buttons. Each is decorative — the button carries the accessible name via
 * `aria-label`/`title` — so the SVG is marked `aria-hidden`.
 */
function Icon({ children, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className="w-4 h-4"
      {...props}
    >
      {children}
    </svg>
  );
}

/** Filled play triangle — start a remote session. */
export function PlayIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <polygon points="6 3 20 12 6 21 6 3" fill="currentColor" stroke="none" />
    </Icon>
  );
}

/** Clockwise refresh arrow — restart the agent. */
export function RestartIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M21 12a9 9 0 1 1-2.64-6.36" />
      <polyline points="21 3 21 9 15 9" />
    </Icon>
  );
}

/** Checkmark — the agent is already up to date. */
export function CheckIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <polyline points="20 6 9 17 4 12" />
    </Icon>
  );
}

/** Trash can — delete the device. */
export function TrashIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
      <path d="M10 11v6M14 11v6" />
      <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
    </Icon>
  );
}

/** Wrench — enter maintenance (suppress telemetry). */
export function WrenchIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <path d="M14.7 6.3a4 4 0 0 0-5.4 5.3L3 18v3h3l6.4-6.3a4 4 0 0 0 5.3-5.4l-2.9 2.9-2.4-.6-.6-2.4 2.9-2.9z" />
    </Icon>
  );
}

/** Pulse line — resume monitoring (exit maintenance). */
export function ActivityIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon {...props}>
      <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
    </Icon>
  );
}

/** Spinning ring — an action is in flight. */
export function SpinnerIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <Icon className="w-4 h-4 animate-spin" {...props}>
      <path d="M21 12a9 9 0 1 1-2.64-6.36" />
    </Icon>
  );
}
