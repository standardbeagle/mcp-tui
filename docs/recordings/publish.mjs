// Publish a recorded demo into the docs site.
//
//   node docs/recordings/publish.mjs <name>
//
// Reads the engine's output, demos/<name>/out/<name>.webm (plus <name>.vtt
// captions when the demo is narrated), and writes:
//
//   <name>-<hash>.webm, <name>-<hash>.mp4
//                                    the video (VP9, and H.264 for Safari),
//                                    uploaded to the docs-media release and
//                                    copied to docs/public/videos (git-ignored)
//   docs/public/videos/<name>.webp   poster frame
//   docs/public/videos/<name>.vtt    captions, when there are any
//   docs/src/assets/recordings/<name>.webp
//                                    animated loop for the README, only for
//                                    entries with "readmeLoop" set
//
// The entry for <name> in docs/src/data/videos.json carries the editorial
// fields (title, description, posterAt, readmeLoop {from, to}). This script fills in
// what it measures: duration (ISO 8601, for VideoObject) and uploadDate.
// A demo with no entry is refused: every published video needs a title and
// description for the page and for search engines.
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
// The release that holds the docs' video files.
const MEDIA_RELEASE = 'docs-media';
const docs = path.resolve(here, '..');
const name = process.argv[2];
if (!name) {
  console.error('usage: node docs/recordings/publish.mjs <demo-name>');
  process.exit(2);
}

const dataPath = path.join(docs, 'src/data/videos.json');
const videos = JSON.parse(fs.readFileSync(dataPath, 'utf8'));
const entry = videos[name];
if (!entry) {
  console.error(`no entry for "${name}" in ${dataPath}: add its title and description first`);
  process.exit(1);
}

const outDir = path.join(here, 'demos', name, 'out');
const source = path.join(outDir, `${name}.webm`);
if (!fs.existsSync(source)) {
  console.error(`missing ${source}: record it first (make demo NAME=${name})`);
  process.exit(1);
}

const run = (cmd, args) => execFileSync(cmd, args, {stdio: ['ignore', 'pipe', 'inherit']}).toString().trim();
const ffmpeg = (args) => run('ffmpeg', ['-y', '-loglevel', 'error', ...args]);

const seconds = Number(run('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', source]));
if (!Number.isFinite(seconds) || seconds <= 0) {
  console.error(`ffprobe could not read a duration from ${source}`);
  process.exit(1);
}

const publicDir = path.join(docs, 'public/videos');
fs.mkdirSync(publicDir, {recursive: true});
const out = (ext) => path.join(publicDir, `${name}.${ext}`);

// The video files are too large for git: they are release assets of the
// docs-media release, named by content hash so a re-record never collides
// with a cached copy, and the docs build downloads them (make docs-media;
// the docs workflow does the same) into public/videos, which git ignores.
const mp4Tmp = path.join(outDir, `${name}.mp4`);
ffmpeg(['-i', source, '-c:v', 'libx264', '-preset', 'slow', '-crf', '26', '-pix_fmt', 'yuv420p',
  '-movflags', '+faststart', '-c:a', 'aac', '-b:a', '96k', mp4Tmp]);
const hashed = (file, ext) =>
  `${name}-${createHash('sha256').update(fs.readFileSync(file)).digest('hex').slice(0, 10)}.${ext}`;
const files = {webm: hashed(source, 'webm'), mp4: hashed(mp4Tmp, 'mp4')};
fs.copyFileSync(source, path.join(publicDir, files.webm));
fs.copyFileSync(mp4Tmp, path.join(publicDir, files.mp4));

const repo = 'standardbeagle/mcp-tui';
run('gh', ['release', 'upload', MEDIA_RELEASE, '--repo', repo, '--clobber',
  path.join(publicDir, files.webm), path.join(publicDir, files.mp4)]);
const assets = JSON.parse(run('gh', ['release', 'view', MEDIA_RELEASE, '--repo', repo, '--json', 'assets'])).assets;
const keep = new Set(Object.values(files));
for (const asset of assets) {
  if (new RegExp(`^${name}-[0-9a-f]{10}\\.(webm|mp4)$`).test(asset.name) && !keep.has(asset.name)) {
    run('gh', ['release', 'delete-asset', MEDIA_RELEASE, asset.name, '--repo', repo, '--yes']);
  }
}
for (const stale of fs.readdirSync(publicDir)) {
  if (new RegExp(`^${name}(-[0-9a-f]{10})?\\.(webm|mp4)$`).test(stale) && !keep.has(stale)) {
    fs.rmSync(path.join(publicDir, stale));
  }
}

const posterAt = Math.min(entry.posterAt ?? seconds * 0.6, seconds - 0.1);
ffmpeg(['-ss', String(posterAt), '-i', source, '-frames:v', '1', '-vf', 'scale=1280:-1', '-quality', '85', out('webp')]);

const captions = path.join(outDir, `${name}.vtt`);
if (fs.existsSync(captions)) fs.copyFileSync(captions, out('vtt'));
else fs.rmSync(out('vtt'), {force: true});

// A README loop is a short excerpt: readmeLoop is {from, to} in seconds.
if (entry.readmeLoop) {
  const {from, to} = entry.readmeLoop;
  if (!(from >= 0 && to > from && to <= seconds)) {
    console.error(`${name}: readmeLoop needs 0 <= from < to <= ${seconds.toFixed(1)}, got ${JSON.stringify(entry.readmeLoop)}`);
    process.exit(1);
  }
  const loop = path.join(docs, 'src/assets/recordings', `${name}.webp`);
  fs.mkdirSync(path.dirname(loop), {recursive: true});
  ffmpeg(['-ss', String(from), '-t', String(to - from), '-i', source, '-vf', 'fps=8,scale=960:-1:flags=lanczos',
    '-loop', '0', '-quality', '70', '-compression_level', '4', loop]);
}

const iso = (s) => {
  const m = Math.floor(s / 60);
  const rest = Math.round(s - m * 60);
  return m > 0 ? `PT${m}M${rest}S` : `PT${rest}S`;
};
videos[name] = {
  ...entry,
  files,
  duration: iso(seconds),
  uploadDate: new Date().toISOString().slice(0, 10),
  captions: fs.existsSync(captions),
};
fs.writeFileSync(dataPath, JSON.stringify(videos, null, 2) + '\n');

const size = (p) => `${(fs.statSync(p).size / 1024 / 1024).toFixed(1)} MB`;
console.log(`published ${name}: ${seconds.toFixed(1)}s, ${files.webm} ${size(path.join(publicDir, files.webm))}, ${files.mp4} ${size(path.join(publicDir, files.mp4))}`);
