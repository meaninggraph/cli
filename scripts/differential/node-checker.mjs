// The Node reference checker of github.com/meaninggraph/core, wrapped to give a
// verdict on one item: a directory of meaning files and, in item.json, what the
// command line needs to know about it (see testdata/corpus).
//
//   const checker = await createChecker('/path/to/core');   // npm ci done
//   checker.problemsOf(item, dir)  // the problems the reference checker reports
//   checker.verdictOf(item, dir)   // { verdict: 'accept' | 'refuse', crashed?, problems }
//
// Used by run.mjs (the corpus) and by mutation/mutate.mjs (random text mutants).
import { cpSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export async function createChecker(coreArg, { graphRoot = '/' } = {}) {
  const core = resolve(coreArg);
  const load = (path) => import(pathToFileURL(join(core, path)).href);
  const meaning = await load('scripts/lib/meaning.mjs');
  const { checkCore } = await load('scripts/check.mjs');
  const schemaPath = join(core, 'meaning.schema.json');

  function problemsOf(item, dir) {
    if (item.profile === 'universal') {
      // core's own check (check.mjs) adds the rules of a repository of universal concepts.
      const copy = mkdtempSync(join(tmpdir(), 'meaning-differential-'));
      try {
        cpSync(dir, copy, { recursive: true });
        cpSync(schemaPath, join(copy, 'meaning.schema.json'));
        return checkCore(copy).problems.map((p) => p.replaceAll(`${copy}/`, ''));
      } finally {
        rmSync(copy, { recursive: true, force: true });
      }
    }
    const at = item.check ? join(dir, item.check) : dir;
    const graphs = item.graphs ?? {};
    // Paths in item.graphs are relative to graphRoot (the repository root for the
    // corpus, '/' for absolute paths).
    const sources = Object.fromEntries(Object.entries(graphs).map(([address, path]) => [address, { dir: path }]));
    const read = meaning.createResolver({ root: graphRoot, sources, cacheDir: join(tmpdir(), 'meaning-differential-unused') });
    // Another graph read from git needs a ?ref= pin; a directory stands in for it, so the pin is required here too.
    const resolveGraph = (repo, ref) => (graphs[repo] && !ref ? { error: `meaning://${repo} is read from git and needs a ?ref= pin` } : read(repo, ref));
    const local = meaning.loadMeaningDir(at, item.address);
    return meaning.checkMeaning({ local, resolve: resolveGraph, schemaPath, selfRepo: item.address }).map((p) => p.replaceAll(`${at}/`, ''));
  }

  function verdictOf(item, dir) {
    try {
      const problems = problemsOf(item, dir).map((p) => p.replaceAll(`${dir}/`, ''));
      return { verdict: problems.length === 0 ? 'accept' : 'refuse', problems };
    } catch (error) {
      // The reference checker throws on a file that is not YAML (and on some shapes the
      // schema would have refused): no verdict but a refusal.
      return { verdict: 'refuse', crashed: true, problems: [String(error.message).split('\n')[0]] };
    }
  }

  return { core, problemsOf, verdictOf };
}
