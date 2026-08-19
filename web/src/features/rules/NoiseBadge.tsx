import type { components } from '../../types/api';
import { noiseTone, noiseWording } from './rule-summary';

type Noise = components['schemas']['RuleNoise'];

/**
 * How much a rule has been raising lately, coloured against its own usual rate.
 *
 * The comparison is the whole point. A rule meant to fire forty times a day
 * firing forty times a day is the system working; the same forty on a rule that
 * normally fires twice is what somebody has to look at. A badge against a shared
 * threshold would sit permanently red on the chatty rules and be ignored.
 */
export function NoiseBadge({ noise }: { readonly noise: Noise }) {
  return (
    <span
      className={`px-2 py-0.5 rounded text-xs tabular-nums ${noiseTone(noise.level)}`}
      title={noiseWording(noise)}
    >
      {noise.recent}
    </span>
  );
}
