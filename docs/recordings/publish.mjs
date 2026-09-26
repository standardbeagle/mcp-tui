// Publish a recorded demo into the docs site.
//
//   node docs/recordings/publish.mjs <name>
//
// Reads the engine's output, demos/<name>/out/<name>.webm (plus the .webm.vtt
// captions when the demo is narrated), and writes:
//
//   docs/public/videos/<name>.webm   the video as recorded (VP9)
//   docs/public/videos/<name>.mp4    H.264 for Safari and for sharing
//   docs/public/videos/<name>.webp   poster frame
//   docs/public/videos/<name>.vtt    captions, when there are any
//   docs/src/assets/recordings/<name>.webp
//                                    animated loop for the README, only for
//                                    entries with "readmeLoop" set
//
// The entry for <name> in docs/src/data/videos.json carries the editorial
// fields (title, description, posterAt, readmeLoop). This script fills in
// what it measures: duration (ISO 8601, for VideoObject) and uploadDate.
// A demo with no entry is refused: every published video needs a title and
// description for the page and for search engines.
import {execFileSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
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

fs.copyFileSync(source, out('webm'));
ffmpeg(['-i', source, '-c:v', 'libx264', '-preset', 'slow', '-crf', '26', '-pix_fmt', 'yuv420p',
  '-movflags', '+faststart', '-c:a', 'aac', '-b:a', '96k', out('mp4')]);

const posterAt = Math.min(entry.posterAt ?? seconds * 0.6, seconds - 0.1);
ffmpeg(['-ss', String(posterAt), '-i', source, '-frames:v', '1', '-vf', 'scale=1280:-1', '-quality', '85', out('webp')]);

const captions = `${source}.vtt`;
if (fs.existsSync(captions)) fs.copyFileSync(captions, out('vtt'));
else fs.rmSync(out('vtt'), {force: true});

if (entry.readmeLoop) {
  const loop = path.join(docs, 'src/assets/recordings', `${name}.webp`);
  fs.mkdirSync(path.dirname(loop), {recursive: true});
  ffmpeg(['-i', source, '-vf', 'fps=8,scale=960:-1:flags=lanczos', '-loop', '0', '-quality', '70',
    '-compression_level', '6', loop]);
}

const iso = (s) => {
  const m = Math.floor(s / 60);
  const rest = Math.round(s - m * 60);
  return m > 0 ? `PT${m}M${rest}S` : `PT${rest}S`;
};
videos[name] = {
  ...entry,
  duration: iso(seconds),
  uploadDate: new Date().toISOString().slice(0, 10),
  captions: fs.existsSync(captions),
};
fs.writeFileSync(dataPath, JSON.stringify(videos, null, 2) + '\n');

const size = (p) => `${(fs.statSync(p).size / 1024 / 1024).toFixed(1)} MB`;
console.log(`published ${name}: ${seconds.toFixed(1)}s, webm ${size(out('webm'))}, mp4 ${size(out('mp4'))}`);
