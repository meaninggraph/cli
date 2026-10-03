// Runs the Node reference checker of github.com/meaninggraph/core over every
// item of testdata/corpus and records its verdicts, with the core commit, in
// testdata/golden/node-verdicts.json. The Go tests compare `meaninggraph check`
// with that file; they never run Node themselves.
//
//   node scripts/differential/run.mjs <core checkout, npm ci done> <output.json>
//
// Use scripts/differential/regenerate.sh, which also checks that the checkout
// and the schema embedded in the binary agree.
import { execFileSync } from 'node:child_process';
import { cpSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const [, , coreArg, outArg] = process.argv;
if (!coreArg || !outArg) {
  console.error('usage: node scripts/differential/run.mjs <core checkout> <output.json>');
  process.exit(2);
}
const core = resolve(coreArg);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const corpus = join(repoRoot, 'testdata', 'corpus');
const load = (path) => import(pathToFileURL(join(core, path)).href);
const meaning = await load('scripts/lib/meaning.mjs');
const { checkCore } = await load('scripts/check.mjs');
const schemaPath = join(core, 'meaning.schema.json');
const version = (pkg) => JSON.parse(readFileSync(join(core, 'node_modules', pkg, 'package.json'), 'utf8')).version;

const status = execFileSync('git', ['-C', core, 'status', '--porcelain']).toString().trim();
if (status) {
  console.error(`the core checkout is not clean:\n${status}`);
  process.exit(2);
}
const commit = execFileSync('git', ['-C', core, 'rev-parse', 'HEAD']).toString().trim();

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
  const sources = Object.fromEntries(Object.entries(graphs).map(([address, path]) => [address, { dir: path }]));
  const read = meaning.createResolver({ root: repoRoot, sources, cacheDir: join(tmpdir(), 'meaning-differential-unused') });
  // Another graph read from git needs a ?ref= pin; a directory stands in for it, so the pin is required here too.
  const resolve = (repo, ref) => (graphs[repo] && !ref ? { error: `meaning://${repo} is read from git and needs a ?ref= pin` } : read(repo, ref));
  const local = meaning.loadMeaningDir(at, item.address);
  return meaning.checkMeaning({ local, resolve, schemaPath, selfRepo: item.address }).map((p) => p.replaceAll(`${at}/`, ''));
}

const items = {};
for (const name of readdirSync(corpus).sort()) {
  const dir = join(corpus, name);
  const item = JSON.parse(readFileSync(join(dir, 'item.json'), 'utf8'));
  try {
    const problems = problemsOf(item, dir).map((p) => p.replaceAll(`${dir}/`, ''));
    items[name] = { verdict: problems.length === 0 ? 'accept' : 'refuse', problems };
  } catch (error) {
    // The reference checker throws on a file that is not YAML: no verdict but a refusal.
    items[name] = { verdict: 'refuse', crashed: true, problems: [String(error.message).split('\n')[0]] };
  }
}

writeFileSync(resolve(outArg), `${JSON.stringify({
  generated_by: 'scripts/differential/regenerate.sh',
  node_checker: {
    repository: 'https://github.com/meaninggraph/core',
    commit,
    node: process.version,
    ajv: version('ajv'),
    ajv_formats: version('ajv-formats'),
    yaml: version('yaml'),
  },
  items,
}, null, 2)}\n`);
console.log(`recorded ${Object.keys(items).length} verdicts of the reference checker at core ${commit}`);
