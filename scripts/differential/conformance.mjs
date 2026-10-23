// Runs the conformance cases of the reference checker (scripts/test-conformance.mjs of github.com/meaninggraph/core: the
// cases of the meaning/draft-2 contract, F format and words, R roles, K kinds, V value sets, X graphs of different
// formats, D derived links, S stored values) and records, for every case, each graph it hands to checkMeaningReport -
// the bytes of its files and of the graphs it refers to - and the report the reference checker gave. The Go tests
// replay the graphs through `meaninggraph check` and `meaninggraph links` and compare; they never run Node.
//
//   node scripts/differential/conformance.mjs <core checkout, npm ci done> <output.json>
//
// Use scripts/differential/regenerate.sh. The cases are the ones core's own test file builds: this script does not
// copy them, it runs that file with two hooks (conformance-hooks.mjs), so a case that core adds or changes is recorded
// by the next run.
import { execFileSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { register } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const [, , coreArg, outArg] = process.argv;
if (!coreArg || !outArg) {
  console.error('usage: node scripts/differential/conformance.mjs <core checkout> <output.json>');
  process.exit(2);
}
const core = resolve(coreArg);
const status = execFileSync('git', ['-C', core, 'status', '--porcelain']).toString().trim();
if (status) {
  console.error(`the core checkout is not clean:\n${status}`);
  process.exit(2);
}
const commit = execFileSync('git', ['-C', core, 'rev-parse', 'HEAD']).toString().trim();

// The graph in a directory as the Go tests need it: the files that a graph is made of. The schemas are not part of it.
const readGraph = (dir) => Object.fromEntries(readdirSync(dir, { withFileTypes: true })
  .filter((entry) => entry.isFile() && !/\.schema\.json$/.test(entry.name))
  .map((entry) => [entry.name, readFileSync(join(dir, entry.name), 'utf8')]));
const schemaDraft2 = (schemaPath) => join(dirname(schemaPath), 'meaning.draft-2.schema.json');

const queue = [];
const afters = [];
let current = null;
globalThis.__conformance = {
  test: (name, fn) => queue.push({ name, fn }),
  after: (fn) => afters.push(fn),
  record(args, asked, report) {
    if (!current || !args.local.dir) return; // a graph that was not read from a directory cannot be replayed
    const call = {
      address: args.selfRepo ?? args.local.address ?? null,
      files: readGraph(args.local.dir),
      // The graphs the check asked for, each once (it asks again for every reference).
      graphs: [...new Map(asked.filter((entry) => entry.dir).map((entry) => [`${entry.repo}@${entry.ref ?? ''}`, { address: entry.repo, pin: entry.ref ?? '', files: readGraph(entry.dir) }])).values()],
      with_schema: Boolean(args.schemaPath),
      // The reference checker reads the schema of draft 2 from beside the schema it is given: a call whose schema path has
      // none beside it is about that lookup, which the embedded schemas of the Go checker do not have.
      reference_only: args.schemaPath && !existsSync(schemaDraft2(args.schemaPath)) ? 'the schema of draft 2 is looked up beside the schema path' : undefined,
      derive: Boolean(args.derive),
      reference: { problems: report.problems, notices: report.notices, links: report.links },
    };
    if (!current.calls.some((seen) => JSON.stringify({ ...seen, reference: 0 }) === JSON.stringify({ ...call, reference: 0 }))) current.calls.push(call);
  },
};

register('./conformance-hooks.mjs', pathToFileURL(`${fileURLToPath(import.meta.url)}`));
await import(pathToFileURL(join(core, 'scripts', 'test-conformance.mjs')).href);

const cases = {};
for (const entry of queue) {
  current = { title: entry.name, calls: [] };
  let failure;
  try { await entry.fn(); } catch (error) { failure = String(error.message ?? error).split('\n')[0]; }
  const id = /^([A-Z]+-\d+)(?: and (?:[A-Z]+-\d+|[\w.]+))?[:,]/.exec(entry.name)?.[1] ?? entry.name.replace(/[^A-Za-z0-9]+/g, '-').replace(/^-|-$/g, '').toLowerCase().slice(0, 60);
  let key = id;
  for (let n = 2; cases[key]; n++) key = `${id}.${n}`;
  cases[key] = { title: current.title, ...(failure ? { reference_failed: failure } : {}), calls: current.calls };
  current = null;
}
for (const fn of afters) fn();
const failed = Object.entries(cases).filter(([, found]) => found.reference_failed);
if (failed.length) {
  console.error(`the reference checker's own cases fail at this commit:\n${failed.map(([key, found]) => `${key}: ${found.reference_failed}`).join('\n')}`);
  process.exit(1);
}

const version = (pkg) => JSON.parse(readFileSync(join(core, 'node_modules', pkg, 'package.json'), 'utf8')).version;
writeFileSync(resolve(outArg), `${JSON.stringify({
  generated_by: 'scripts/differential/regenerate.sh',
  node_checker: { repository: 'https://github.com/meaninggraph/core', commit, node: process.version, ajv: version('ajv'), ajv_formats: version('ajv-formats'), yaml: version('yaml') },
  cases,
}, null, 2)}\n`);
const calls = Object.values(cases).reduce((sum, found) => sum + found.calls.length, 0);
console.log(`recorded ${calls} graphs of ${Object.keys(cases).length} conformance cases of the reference checker at core ${commit}`);
