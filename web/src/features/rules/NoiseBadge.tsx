import type { components } from '../../types/api';
import { noiseTone, noiseWording } from './rule-summary';

type Noise = components['schemas']['RuleNoise'];

// Colours a rule's recent raise count against its own usual rate, so chatty rules do not stay red.
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
