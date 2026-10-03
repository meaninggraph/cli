// Records what the reference checker's YAML parser (the `yaml` package of
// github.com/meaninggraph/core, called the way core's meaning.mjs calls it:
// `parse(text)`) reads from
//
//   - every YAML file of testdata/corpus, and
//   - a generated matrix of documents: scalars in every position, block scalars
//     with every ending of the file, tabs in every position, byte order marks,
//     line ends,
//
// as canonical JSON in testdata/golden/node-values.json. A default Go test
// (pkg/meaning/values_test.go) reads that file and compares what ParseYAML reads
// from the same text with it, field for field; it needs no Node.
//
//   node scripts/differential/values.mjs <core checkout, npm ci done> <output.json>
//
// Run through scripts/differential/regenerate.sh. The matrix is generated, not
// random: the same input gives the same file.
import { execFileSync } from 'node:child_process';
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const [, , coreArg, outArg] = process.argv;
if (!coreArg || !outArg) {
  console.error('usage: node scripts/differential/values.mjs <core checkout> <output.json>');
  process.exit(2);
}
const core = resolve(coreArg);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const yaml = await import(pathToFileURL(join(core, 'node_modules', 'yaml', 'dist', 'index.js')).href);
process.removeAllListeners('warning');
process.on('warning', () => {});

// canonical turns a parsed value into JSON that keeps what a Go comparison needs:
// a number that JSON cannot hold becomes {"$number": "..."}.
function canonical(value) {
  if (typeof value === 'number' && !Number.isFinite(value)) return { $number: String(value) };
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, canonical(v)]));
  return value === undefined ? null : value;
}

function read(text) {
  try {
    return { value: canonical(yaml.parse(text)) };
  } catch (error) {
    return { refused: String(error.message).split('\n')[0] };
  }
}

const entries = [];
const add = (group, name, text) => entries.push({ group, name, text, ...read(text) });

// ---- the corpus: every YAML file, by path (the text is read from the corpus at test time)
const corpus = join(repoRoot, 'testdata', 'corpus');
const corpusFiles = [];
const walk = (dir) => {
  for (const name of readdirSync(dir).sort()) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path);
    else if (name.endsWith('.yaml')) corpusFiles.push(path);
  }
};
walk(corpus);
for (const path of corpusFiles) {
  const result = read(readFileSync(path, 'utf8'));
  entries.push({ group: 'corpus', name: relative(repoRoot, path), ...result });
}

// ---- scalars in every position
const scalars = [
  'x', 'hello world', 'a b  c', 'x#y', 'a:b', 'http://x/y?z=1', '?x', '-x', ':x', 'x &y', '5%', 'a, b', 'x] y', 'x*y', 'x!y', '@x', 'a=b', 'a|b', 'a>b', "it's", 'say "hi"',
  '840', '-3', '+5', '007', '1.5', '.5', '5.', '1e3', '1E3', '1e-3', '-0', '0', '1_000', '0b1', '2025-01-01', '12:30', '1.2.3',
  'true', 'True', 'TRUE', 'false', 'False', 'FALSE', 'yes', 'No', 'on', 'Off', 'y', 'n',
  'null', 'Null', 'NULL', '~', 'nil', '',
  '0x1F', '0o17', '-0x1F', '0o89', '.inf', '-.inf', '.nan', '9007199254740992', '9007199254740993', '1e999',
  '"quoted"', "'single'", '"a\\tb"', '"a\\nb"', '"\\u00e9"', '"\\/"', '"\\uD83D\\uDE00"', '""', "''", "'it''s'", '"say \\"hi\\""', '"# not a comment"', '"a: b"',
  'é', '日本語', '😀',
];
const positions = {
  'map-value': (s) => `a: ${s}\nb: 1\n`,
  'map-value-nested': (s) => `a:\n  b: ${s}\n  c: 2\nd: 3\n`,
  'seq-item': (s) => `- ${s}\n- 2\n`,
  'seq-under-key': (s) => `a:\n- ${s}\n- 2\n`,
  'seq-of-maps': (s) => `- a: ${s}\n  b: 1\n`,
  'flow-seq': (s) => `a: [${s}, 2]\n`,
  'flow-map': (s) => `a: {k: ${s}, l: 2}\n`,
  'flow-map-quoted-key': (s) => `a: {"k": ${s}}\n`,
  'key': (s) => `${s}: 1\nb: 2\n`,
  'flow-key': (s) => `a: {${s}: 1}\n`,
  'root': (s) => `${s}\n`,
  'with-comment': (s) => `a: ${s} # comment\nb: 1\n`,
};
for (const [position, make] of Object.entries(positions)) {
  for (const scalar of scalars) add('scalar', `${position}: ${JSON.stringify(scalar)}`, make(scalar));
}

// ---- plain scalars over lines
const wrapped = [
  'a: one\n  two\n',
  'a: one\n  two\n   three\nb: 1\n',
  'a: one\n\n  two\n',
  'a: one\n\n\n  two\nb: 1\n',
  'a:\n  one\n  two\n',
  'a:\n  one\n\n  two\n',
  '- one\n  two\n- three\n',
  '- one\n\n  two\n',
  'a: one\n  two # c\nb: 1\n',
  'a: one\n  - two\n',
  'a: x\n  y: z\n',
  'a: [one,\n  two]\n',
  'a: {k: one,\n  l: two}\n',
  'a: one\n# comment\n  two\n',
];
for (const text of wrapped) add('plain-lines', JSON.stringify(text), text);

// ---- block scalars: every style, body and ending of the file
const styles = ['|', '>', '|-', '>-', '|+', '>+', '|2', '>-2', '|2-', '>+2'];
const bodies = {
  one: ['text'],
  blank: ['one', '', 'two'],
  two_blank: ['one', '', '', 'two'],
  more: ['one', ' two', 'three'],
  hash: ['# not a comment', 'x   '],
};
const endings = {
  none: () => '',
  nl: () => '\n',
  nl3: () => '\n\n\n',
  last_spaces: (ind) => `\n${' '.repeat(ind + 2)}`,
  last_spaces_less: (ind) => `\n${' '.repeat(Math.max(1, ind - 1))}`,
  last_spaces_exact: (ind) => `\n${' '.repeat(ind)}`,
  nl_then_spaces: (ind) => `\n\n${' '.repeat(ind)}`,
  spaces_1: () => `\n `,
  spaces_4: () => `\n    `,
  spaces_6: () => `\n      `,
  spaces_8: () => `\n        `,
  blank_spaces_nl: (ind) => `\n${' '.repeat(ind)}\n`,
  tab_only: () => '\n\t',
  tab_nl: () => '\n\t\n',
  indent_tab: (ind) => `\n${' '.repeat(ind)}\t`,
  comment: () => '\n# end',
  comment_indented: (ind) => `\n${' '.repeat(ind)}# end`,
  comment_nl: () => '\n# end\n',
  next_key: () => '\nb: 1',
  next_key_nl: () => '\nb: 1\n',
  blank_next_key: () => '\n\nb: 1\n',
  crlf: () => '\r\n',
  crlf_spaces: (ind) => `\r\n${' '.repeat(ind + 2)}`,
};
const blockPositions = {
  'map-value': { head: (s) => `a: ${s}\n`, indent: 2 },
  'nested-map': { head: (s) => `m:\n  a: ${s}\n`, indent: 4 },
  'seq-item': { head: (s) => `- ${s}\n`, indent: 2 },
};
for (const style of styles) {
  const group = style.includes('+') ? 'block-keep' : /\d/.test(style) ? 'block-indicator' : 'block-eof';
  for (const [bodyName, body] of Object.entries(bodies)) {
    for (const [position, spec] of Object.entries(blockPositions)) {
      for (const [endingName, ending] of Object.entries(endings)) {
        // The text of the scalar is indented by `indent`; the ending follows its last line.
        const lines = body.map((line) => (line === '' ? '' : ' '.repeat(spec.indent) + line)).join('\n');
        const text = spec.head(style) + lines + ending(spec.indent);
        add(group, `${style} ${bodyName} in ${position} ending ${endingName}`, text);
      }
    }
  }
}

// ---- tabs in every position
const tabCases = {
  // outside any scalar
  'comment line': ['a: 1\n# a\tb\nb: 2\n', 'a: 1\n#\tx\nb: 2\n', '# a\tb\na: 1\n', 'a: 1\n  # a\tb\nb: 2\n', '- x\n# a\tb\n- y\n'],
  'comment after value': ['a: 1 # a\tb\n', 'a: "q" # a\tb\n', 'a: [x] # a\tb\n', 'a: | # a\tb\n  x\n', 'a: 1 #\tb\nb: 2\n'],
  'blank line': ['a: 1\n\t\nb: 2\n', 'a: 1\n \t \nb: 2\n', '\t\na: 1\n', 'a: 1\n\t', 'a: 1\n\t\n', '- x\n\t\n- y\n', 'a:\n  b: 1\n\t\n  c: 2\n', 'a: one\n\t\n  two\n', 'a: [x,\n\t\n  y]\n'],
  'trailing': ['a: 1\t\n', 'a: x\t\n', 'a: x \t \n', 'a: "q"\t\n', "a: 'q'\t\n", 'a: [x]\t\n', 'a: {k: v}\t\n', 'a:\t\n', 'a: |\t\n  x\n', 'a: >-\t\n  x\n', '- x\t\n', '-\t\n', 'a: x\t', 'a: x \t', 'a:\n  b: 1\t\n', '"k"\t: 1\n', 'k\t: 1\n', 'a: [x,\t\n  y]\n', 'a: x\n  y\t\n'],
  // between tokens
  'after colon': ['a:\t1\n', 'a:\tx\n', 'a: \t1\n', 'a:\t"q"\n', 'a:\t[x]\n', 'a:\t|\n  x\n', 'a:\t# c\n', 'a:\t\n  b: 1\n'],
  'after dash': ['-\tx\n', '- \tx\n', '-\t"q"\n', '-\ta: 1\n', '-\t\n  x\n'],
  'before comment': ['a: x\t# c\n', 'a: "q"\t# c\n', 'a: [x]\t# c\n', 'a: 1\t#c\nb: 2\n'],
  'indentation': ['a:\n\tb: 1\n', 'a:\n \tb: 1\n', '\ta: 1\n', 'a: 1\n\tb: 2\n', 'a:\n  b: 1\n\tc: 2\n', '- x\n\t- y\n', 'a:\n\t- x\n', 'a: |\n\tx\n', 'a: |\n  x\n\ty\n', 'a: one\n\ttwo\n', 'a:\n  b: |\n    x\n  \tc: 1\n'],
  'in flow': ['a: [x,\ty]\n', 'a: [\tx]\n', 'a: {k:\tv}\n', 'a: {k\t: v}\n', 'a: [x\t]\n', 'a: {k: v,\tl: w}\n', 'a: [x]\t\n', 'a: [x,\n\ty]\n', 'a: [\t]\n', 'a: {\t}\n'],
  // inside a scalar
  'in double quotes': ['a: "x\ty"\n', 'a: "\tx"\n', 'a: "x\t"\n', 'a: "\t"\n', '"k\tk": 1\n', 'a: {"k\tk": "v\tv"}\n', 'a: ["x\ty"]\n', '- "x\ty"\n', 'a: "x\t#y"\n', 'a: "x\t: y"\n'],
  'in single quotes': ["a: 'x\ty'\n", "a: '\tx'\n", "a: 'x\t'\n", "a: '\t'\n", "'k\tk': 1\n", "a: {'k\tk': 'v\tv'}\n", "a: ['x\ty']\n", "- 'x\ty'\n", "a: 'x\t#y'\n"],
  'in plain scalars': ['a: x\ty\n', 'a: x\ty # c\n', 'a: x\t-\n', 'a: x\t y\n', 'a: x \ty\n', 'a: x\t\ty\n', 'a: x\ty z\n', '- x\ty\n', 'a: x\ty\nb: 1\n', 'k\tk: 1\n', 'k\tk: v\tv\n', 'a: x\ty # c\n', 'a: x\t#y\n', 'a: x\t# c\n', 'a: 1\t2\n', 'a: true\tx\n', 'a: x:\ty\n', 'a: x\t: y\n', 'a: x\n  y\tz\n', 'a: x\ty\n  z\n', 'x\ty\n', 'a: -\tx\n', 'a: x\t-\n', 'a: ?\tx\n', 'a: [x\ty]\n', 'a: {k: x\ty}\n', 'a: {k\tk: v}\n'],
  'after a block scalar': ['a: |\n  x\n\t', 'a: |\n  x\nb: 1\n\t\n', 'a: |\n  x\n# c\n\t', 'a: |\n  x\nb: 1\n\t', 'a: |\n  x\nb: 1\n \t\n', 'a: |\n  x\nb: 1\n\n\t\nc: 2\n', 'a: |\n  x\nb: |\n  y\n\t\n', 'a: >-\n  x\n # c\n\t', 'a: >-\n  x\n  # c\n\t', 'a: 1\n# c\n\t', 'a: |\n  x\n\t# c\n', 'a: 1\n\t# c\nb: 2\n', 'a: |\n  x\nb: 1\n\t# c\n', '- |\n  x\n\t\n- y\n', 'a: |\n  x\n  \t\nb: 1\n', 'a: |\n  x\n\n\t\n', 'a:\n  b: |\n    x\n\t\n  c: 1\n', 'a: |\n  x\nb: 1\n\t\nc: 2\n', 'a: |\n  x\n# a\tb\nc: 1\n', 'a: |\n  x\n  \t# c\nc: 1\n', 'a: |\n    x\n  # c\n\t', 'a: |\n    x\n  # c\n  \t\nb: 1\n', 'a: |\n  x\n\n# c\n\t\n', 'a:\n  - |\n    x\n  # c\n\t\n  - y\n', 'a: |\n  x\n#\tc\n\t# d\n'],
  'comment line indentation': ['a: 1\n\t# c\nb: 2\n', 'a:\n\t# c\n  b: 1\n', '- x\n\t# c\n- y\n', 'a: 1\n \t# c\nb: 2\n', '\t# c\na: 1\n', 'a: "q"\n\t# c\nb: 2\n', 'a: [x]\n\t# c\nb: 2\n', 'a: x\n\t# c\nb: 2\n', 'a: x\n  y\n\t# c\nb: 2\n', 'a: 1\n\t# c\n\t# d\nb: 2\n', 'a: 1\n\t\n\t# d\nb: 2\n'],
  'in block text': ['a: >\n  x\n  \ty\n  z\n', 'a: >\n  \tx\n  y\n', 'a: >-\n  x\n\n  \ty\n','a: |\n  x\ty\n', 'a: |\n  \tx\n', 'a: |\n  x\t\n', 'a: |-\n  x\t\n', 'a: >\n  x\ty\n  z\n', 'a: >-\n  x\t\n  y\n', 'a: |\n  x\n  \t\n  y\n', 'a: |\n  x\n\t\n  y\n', 'a: |\n  x\n  y\t\n\t\n', 'a: |\n  x\n\t\n', 'a: |\n  x\n  \t\n', 'a: |\n  x\n  \t', 'a: |\n  x\n\t', 'a: |\n  x\ty\nb: 1\n', 'a: >\n  x\n   \ty\n  z\n', 'a: |\n  # x\ty\n', '- |\n  x\ty\n- z\n', 'a: |\n  x\n\n\t\n', 'a: |\n  \t\n  x\n', 'a: |\n\n  x\ty\n', 'a: |-\n  x\n  \t\n', 'a: >\n  x\n  \t\n  y\n'],
};
// The tabs that are read (inside quotes, in the text of a comment, between two
// characters of plain and block text) and all the others, which are refused.
const tabsRead = [
  'a: 1\n# a\tb\nb: 2\n', 'a: 1\n#\tx\nb: 2\n', '# a\tb\na: 1\n', 'a: 1\n  # a\tb\nb: 2\n', '- x\n# a\tb\n- y\n', 'a: 1 # a\tb\n', 'a: "q" # a\tb\n', 'a: [x] # a\tb\n', 'a: | # a\tb\n  x\n', 'a: 1 #\tb\nb: 2\n', 'a: |\n  x\n# a\tb\nc: 1\n',
  'a: "x\ty"\n', 'a: "\tx"\n', 'a: "x\t"\n', 'a: "\t"\n', '"k\tk": 1\n', 'a: {"k\tk": "v\tv"}\n', 'a: ["x\ty"]\n', '- "x\ty"\n', 'a: "x\t#y"\n', 'a: "x\t: y"\n',
  "a: 'x\ty'\n", "a: '\tx'\n", "a: 'x\t'\n", "a: '\t'\n", "'k\tk': 1\n", "a: {'k\tk': 'v\tv'}\n", "a: ['x\ty']\n", "- 'x\ty'\n", "a: 'x\t#y'\n",
  'a: x\ty\n', 'a: x\ty # c\n', 'a: x\t-\n', 'a: x\t y\n', 'a: x \ty\n', 'a: x\t\ty\n', 'a: x\ty z\n', '- x\ty\n', 'a: x\ty\nb: 1\n', 'a: 1\t2\n', 'a: true\tx\n', 'a: x\n  y\tz\n', 'a: x\ty\n  z\n', 'x\ty\n',
  'a: |\n  x\ty\n', 'a: >\n  x\ty\n  z\n', 'a: |\n  x\ty\nb: 1\n', 'a: |\n  # x\ty\n', '- |\n  x\ty\n- z\n', 'a: |\n\n  x\ty\n', 'a: |\n    a\tb  \n', 'a: |-\n  x\ty\n  z\n',
];
for (const text of tabsRead) add('tab read', JSON.stringify(text), text);
const readSet = new Set(tabsRead);
const tabsRefused = [...new Set(Object.values(tabCases).flat())].filter((text) => !readSet.has(text));
tabsRefused.push(
  // the reviewer's cases: a comment line or a line of tabs where the reference parser acts on it
  'a:\n\t# c\n  one\nb: 1\n', '-\n\t# c\n  one\n- two\n', 'a: |\n\t# c\nb: 1\n', 'a:\n  b: |\n  \t# c\n  c: 1\n', 'a:\n  b: 1\n  # c\n\t', 'c:\n\t\nd: 1\n', 'c:\n\t',
  'a: 1\n\t\n', 'a: 1\n\t', 'a: 1\n \t', 'a: 1\n  \t\n', 'a:\n  b: 1\n\t# more to come\n', 'a:\n  b: 1\n  # more\n\t',
);
for (const text of tabsRefused) add('tab refused', JSON.stringify(text), text);

// ---- comment and blank lines in every place of a small document
const fillers = [];
for (const col of [0, 1, 2, 4, 6]) for (const text of ['#c', '# c']) fillers.push(' '.repeat(col) + text);
fillers.push('#', '  #', '    #', '#TODO reword', '# TODO reword', '', '  ', '    ');
fillers.push('\t#c', '\t# c', '  \t# c', '# a\tb', '#\tc', '\t', ' \t', '  \t ', '\t  ');
// Documents with a value on a later line that is a scalar (a comment line before
// it is refused), and with one that starts a collection (a comment line before
// it is everyday YAML and is read, at every column).
const scalarAfter = [
  'a:\n  one\nb: 1\n', '-\n  one\n- two\n', 'a: |\n  text\nb: 1\n', 'a: |\n  text\n', 'a: >-\n  t1\n  t2\nb: 1\n',
  'a: one\n  two\nb: 1\n', '- one\n  two\n- three\n', 'a: [x,\n  y]\n', 'a:\n  "quoted"\nb: 1\n', 'a:\n  [x, y]\nb: 1\n',
  'a: |\nb: 1\n', 'a: |\n\n  text\n', 'a: # c\n  one\nb: 1\n', '- # c\n  one\n- two\n',
  'a: one\n', 'a: |\n  text\n  more\n\nb: 1\n', 'a:\n  b: |\n    x\n  c: 1\n', 'description:\n  A demo graph.\nconcepts:\n  - id: thing\n',
  '- a: 1\n  b:\n    one\n  c: 2\n',
];
const collectionAfter = [
  'a:\n  b: 1\nc: 2\n', 'a:\n  - x\n  - y\n', 'a:\nb: 1\n', 'a:\n  b:\n    c: 1\n', 'a:\n- x\n- y\nb: 1\n', '- - a\n  - b\n',
  'concepts:\n  - id: thing\n    kind: entity\n    description: A thing.\n', '- a: 1\n  c: 2\n- b: 3\n', 'a:\n  b:\n  - x\n  - y\nc: 1\n', 'a:\n  - b: 1\n    c: 2\n  - d: 3\n',
];
for (const [group, baseDocs] of [['comment-lines', scalarAfter], ['comment-lines-collection', collectionAfter]]) for (const doc of baseDocs) {
  const lines = doc.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  for (let at = 0; at <= lines.length; at++) {
    for (const filler of fillers) {
      const copy = [...lines];
      copy.splice(at, 0, filler);
      add(group, `${JSON.stringify(doc)} + ${JSON.stringify(filler)} before line ${at}`, copy.join('\n') + '\n');
      // the filler as the last line, without a line break at the end of the file
      if (at === lines.length) add(group, `${JSON.stringify(doc)} + ${JSON.stringify(filler)} at the end without a line break`, copy.join('\n'));
    }
  }
}

// ---- byte order marks and line ends
const bom = '\ufeff';
const bomBodies = {
  unindented: 'a: 1\nb: 2\n',
  indented: '  a: 1\n  b: 2\n',
  indented_one: ' a: 1\n b: 2\n',
  doc_start: '---\na: 1\n',
  doc_start_indented: '---\n  a: 1\n',
  comment_first: '# c\na: 1\n',
  comment_indented: '  # c\na: 1\n',
  comment_indented_then_indented: '  # c\n  a: 1\n',
  blank_first: '\na: 1\n',
  blank_first_indented: '\n  a: 1\n',
  spaces_first: '  \na: 1\n',
  seq: '- a\n- b\n',
  seq_indented: '  - a\n  - b\n',
  seq_nested: '- a: 1\n  b: 2\n',
  seq_nested_indented: '  - a: 1\n    b: 2\n',
  scalar: 'x\n',
  scalar_indented: '  x\n',
  flow: '[a, b]\n',
  flow_indented: '  [a, b]\n',
  flow_over_lines: '[a,\n  b]\n',
  quoted: '"x"\n',
  quoted_indented: '  "x"\n',
  block: 'a: |\n  x\n',
  block_root_indented: '  a: |\n    x\n',
  root_block_scalar: '|\n  x\n',
  root_block_scalar_folded: '>-\n  x\n  y\n',
  key_then_seq: 'a:\n- x\n- y\n',
  key_then_seq_indented: 'a:\n  - x\n  - y\n',
  quoted_keys: '"a": 1\n"b": 2\n',
  single_quoted_keys: "'a': 1\n'b': 2\n",
  key_then_nested: 'a:\n  b: 1\n  c: 2\nd: 3\n',
  flow_key: 'a: [x,\n  y]\nb: 1\n',
  flow_root_map: '{a: 1,\n b: 2}\n',
  plain_root_lines: 'x\n  y\n',
  plain_root_lines_flush: 'x\ny\n',
  seq_dash_only: '-\n  a\n-\n  b\n',
  seq_then_comment: '- a\n# c\n- b\n',
  key_only: 'a:\n',
  null_scalar: '~\n',
  doc_start_seq: '---\n- a\n- b\n',
  spaces_then_map: ' \na: 1\n',
  tab_first: '\ta: 1\n',
  only_bom: '',
  two_boms: bom + 'a: 1\n',
  bom_in_value: 'a: x\ufeffy\n',
  bom_after_newline: 'a: 1\n\ufeffb: 2\n',
  bom_in_comment: '# \ufeff\na: 1\n',
  bom_in_quotes: 'a: "\ufeff"\n',
};
for (const [name, body] of Object.entries(bomBodies)) {
  add('bom', `with a mark: ${name}`, bom + body);
  add('bom', `without a mark: ${name}`, body);
}
for (const [name, body] of [['map', 'a: 1\nb: |\n  x\n  y\nc: [1, 2]\n'], ['indented', '  a: 1\n  b: 2\n']]) {
  add('line-ends', `crlf ${name}`, body.replaceAll('\n', '\r\n'));
  add('line-ends', `crlf with a mark ${name}`, bom + body.replaceAll('\n', '\r\n'));
  add('line-ends', `lone cr ${name}`, body.replaceAll('\n', '\r'));
}

// ---- structures that meaning files use
const structures = [
  'a:\n- 1\n- 2\nb:\n  - x\n  -   y: 1\n      z: 2\n  - - p\n    - q\n  -\n    r: 1\n  -\n  - # c\n    s: 2\nc:\n  d:\n    e: 1\n',
  '- a: 1\n  b: 2\n- "c": 3\n- \'d\'\n- [e]\n- {f: 1}\n- g: |\n    h\n    i\n  j: 2\n',
  '{"a": [1, 2.5, "x", true, null], "b": {}, "c":1, "d" : 2}\n',
  '[a,\n b,\n]\n',
  '---\na: 1\n',
  '# c\n--- # c\n\na: 1\n',
  'a: [x,\n  y,\n  {b: 1,\n   c: 2},\n  ]\nd: {k: v,\n  l: w\n  }\ne: [\n  1\n  ]\n',
  'p:\n  a: [\n    x,\n  ]\n  b: {k: v,\n  }\n  c: [x] # c\nd: [y,\n]\ne:\n- [z,\n]\n- f: {g: h\n    }\n',
  'a: []\nb: {}\nc: [[], {}]\nd: [ ]\ne: { }\n',
  'a: 1   \n\n\nb:   2\n\n',
  'a:\nb:\n  \nc: # comment\nd: ~\n',
  '- a #b: c\n- "d" # e\n',
  'a: {"a\\u0062": 1, \'c\'\'d\': 2}\n',
  '# only a comment\n',
  '',
  '\n\n',
  'a: 1\n...\n',
  'a: 1\n---\nb: 2\n',
  '%YAML 1.2\n---\na: 1\n',
  'a: &x 1\nb: *x\n',
  'a: !!str 1\n',
  'a: 1\na: 2\n',
  '? a\n: 1\n',
  'a: "x\n  y"\n',
  "a: 'x\n  y'\n",
  'a: [x: 1]\n',
  'a: [x, # c\n  y]\n',
];
for (const text of structures) add('structure', JSON.stringify(text), text);

const commit = execFileSync('git', ['-C', core, 'rev-parse', 'HEAD']).toString().trim();
const version = JSON.parse(readFileSync(join(core, 'node_modules', 'yaml', 'package.json'), 'utf8')).version;
// One entry per line, so that a change to the matrix is a readable diff.
const header = { generated_by: 'scripts/differential/regenerate.sh (values.mjs)', node_checker: { repository: 'https://github.com/meaninggraph/core', commit, node: process.version, yaml: version } };
writeFileSync(resolve(outArg), `{\n"generated_by": ${JSON.stringify(header.generated_by)},\n"node_checker": ${JSON.stringify(header.node_checker)},\n"entries": [\n${entries.map((e) => JSON.stringify(e)).join(',\n')}\n]\n}\n`);
const accepted = entries.filter((e) => 'value' in e).length;
console.log(`recorded the values of ${accepted} documents read by the reference parser (${entries.length - accepted} it refuses), ${entries.length} in all`);
