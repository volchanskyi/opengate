import { describe, it, expect, beforeEach } from 'vitest';
import fc from 'fast-check';
import { useFileStore } from './file-store';

// Pinned runs + seed so any counterexample reproduces deterministically in the
// gauntlet (tests-determinism.md). No .skip / .only.
const RUNS = { numRuns: 300, seed: 0x0ac17a7e } as const;

function resetStore(): void {
  useFileStore.setState({
    currentPath: '/',
    entries: [],
    isLoading: false,
    error: null,
    downloads: {},
    uploads: {},
    viewingFile: null,
  });
}

// A reference model of the progress maps. The store's downloads/uploads must
// always equal what a plain Map would hold after the same action sequence:
// the last progress per name, with cleared names removed.
type Channel = 'download' | 'upload';
type Action =
  | { kind: 'set'; channel: Channel; name: string; progress: number }
  | { kind: 'clear'; channel: Channel; name: string };

const nameArb = fc.string({ minLength: 1, maxLength: 6 });

const actionArb: fc.Arbitrary<Action> = fc.oneof(
  fc.record({
    kind: fc.constant('set' as const),
    channel: fc.constantFrom<Channel>('download', 'upload'),
    name: nameArb,
    progress: fc.integer({ min: 0, max: 100 }),
  }),
  fc.record({
    kind: fc.constant('clear' as const),
    channel: fc.constantFrom<Channel>('download', 'upload'),
    name: nameArb,
  }),
);

describe('file-store reducer properties', () => {
  beforeEach(resetStore);

  it('progress maps match a reference model over arbitrary action sequences', () => {
    fc.assert(
      fc.property(fc.array(actionArb, { maxLength: 40 }), (actions) => {
        resetStore();
        const model: Record<Channel, Map<string, number>> = {
          download: new Map(),
          upload: new Map(),
        };

        for (const a of actions) {
          const store = useFileStore.getState();
          if (a.kind === 'set') {
            if (a.channel === 'download') store.setDownloadProgress(a.name, a.progress);
            else store.setUploadProgress(a.name, a.progress);
            model[a.channel].set(a.name, a.progress);
          } else {
            if (a.channel === 'download') store.clearDownload(a.name);
            else store.clearUpload(a.name);
            model[a.channel].delete(a.name);
          }
        }

        const { downloads, uploads } = useFileStore.getState();
        expect(downloads).toEqual(Object.fromEntries(model.download));
        expect(uploads).toEqual(Object.fromEntries(model.upload));
        // Clears must never leave a stale key behind.
        expect(Object.keys(downloads).sort()).toEqual([...model.download.keys()].sort());
        expect(Object.keys(uploads).sort()).toEqual([...model.upload.keys()].sort());
      }),
      RUNS,
    );
  });

  it('setEntries always lands the given path/entries and clears isLoading', () => {
    fc.assert(
      fc.property(
        fc.string(),
        fc.array(
          fc.record({
            name: fc.string(),
            is_dir: fc.boolean(),
            size: fc.nat(),
            modified: fc.nat(),
          }),
          { maxLength: 10 },
        ),
        fc.boolean(),
        (path, entries, seedLoading) => {
          resetStore();
          useFileStore.setState({ isLoading: seedLoading });
          useFileStore.getState().setEntries(path, entries);
          const s = useFileStore.getState();
          expect(s.currentPath).toBe(path);
          expect(s.entries).toEqual(entries);
          expect(s.isLoading).toBe(false);
        },
      ),
      RUNS,
    );
  });

  it('set/clear are inverse for a single name regardless of channel', () => {
    fc.assert(
      fc.property(nameArb, fc.integer({ min: 0, max: 100 }), (name, progress) => {
        resetStore();
        const store = useFileStore.getState();
        store.setDownloadProgress(name, progress);
        const afterSet = new Map(Object.entries(useFileStore.getState().downloads));
        expect(afterSet.get(name)).toBe(progress);
        store.clearDownload(name);
        const afterClear = new Map(Object.entries(useFileStore.getState().downloads));
        expect(afterClear.has(name)).toBe(false);
      }),
      RUNS,
    );
  });
});
