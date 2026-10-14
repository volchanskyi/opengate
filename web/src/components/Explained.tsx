import { useId, useState } from 'react';

interface ExplainedProps {
  label: string;
  explanation: string;
}

/**
 * A label that explains itself: the explanation shows on hover and on keyboard focus, hides on
 * pointer leave, blur and Escape, and is the label's accessible description throughout.
 */
export function Explained({ label, explanation }: Readonly<ExplainedProps>) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const show = () => setOpen(true);
  const hide = () => setOpen(false);

  return (
    <span className="relative inline-flex" onMouseEnter={show} onMouseLeave={hide}>
      <span
        tabIndex={0}
        aria-describedby={id}
        onFocus={show}
        onBlur={hide}
        onKeyDown={(event) => {
          if (event.key === 'Escape') hide();
        }}
        className="cursor-help underline decoration-dotted underline-offset-2 focus:outline-none focus-visible:ring-1 focus-visible:ring-blue-400 rounded-sm"
      >
        {label}
      </span>
      <span
        id={id}
        role="tooltip"
        hidden={!open}
        className="absolute left-0 top-full z-20 mt-1 w-64 rounded border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs font-normal normal-case text-gray-200 shadow-lg"
      >
        {explanation}
      </span>
    </span>
  );
}
