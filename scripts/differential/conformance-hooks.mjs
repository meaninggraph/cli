// Module hooks for conformance.mjs: they wrap two modules of the core checkout while they load, and change nothing in
// the checkout. In scripts/lib/meaning.mjs, checkMeaningReport records every call it gets and the report it returns
// (globalThis.__conformance.record); in scripts/test-conformance.mjs, `test` and `after` come from the recorder
// instead of node:test, so that the cases run one after the other in this process and each call is filed under its case.
export async function load(url, context, nextLoad) {
  const loaded = await nextLoad(url, context);
  if (loaded.format !== 'module') return loaded;
  if (url.endsWith('/scripts/lib/meaning.mjs')) {
    const source = String(loaded.source);
    const marker = 'export function checkMeaningReport(';
    if (!source.includes(marker)) throw new Error('scripts/lib/meaning.mjs has no exported checkMeaningReport to wrap');
    const wrapped = `${source.replace(marker, 'function __checkMeaningReport(')}
export function checkMeaningReport(args) {
  const asked = [];
  const resolve = (repo, ref) => {
    const found = args.resolve(repo, ref);
    asked.push({ repo, ref, dir: found?.dir, address: found?.address, error: found?.error });
    return found;
  };
  const report = __checkMeaningReport({ ...args, resolve });
  globalThis.__conformance.record(args, asked, report);
  return report;
}
`;
    return { ...loaded, source: wrapped };
  }
  if (url.endsWith('/scripts/test-conformance.mjs')) {
    const source = String(loaded.source);
    const marker = "import { after, test } from 'node:test';";
    if (!source.includes(marker)) throw new Error(`scripts/test-conformance.mjs no longer has the line ${marker}`);
    return { ...loaded, source: source.replace(marker, 'const { after, test } = globalThis.__conformance;') };
  }
  return loaded;
}
