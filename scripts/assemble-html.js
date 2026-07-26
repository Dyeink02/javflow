// toolchain-owner: active desktop HTML assembler; marker=active-toolchain-assemble-html
// Generated desktop HTML assembler for the current Wails frontend build chain.
// It expands renderer partials into the generated index.html consumed by the
// Wails frontend sync step.
//
// Ownership summary:
// 1) compose generated desktop HTML from maintained renderer partials
// 2) keep HTML template expansion out of runtime/frontend bundle code
// 3) emit the deterministic index artifact consumed by Wails sync/build steps
//
// Boundary rule:
// build-time only; runtime code should not import this file.
//
// File map for maintainers:
// 1) output directory + UTF-8/BOM write helpers
// 2) partial/template read helpers
// 3) placeholder replacement + generated index write

const fs = require('fs');
const path = require('path');
const {
  rendererDir,
  rendererGeneratedDir,
  rendererPartialsDir,
  generatedIndexHtmlPath
} = require('./frontend-paths');

function ensureDirectory(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
}

function writeUtf8WithBom(filePath, content) {
  const utf8Bom = Buffer.from([0xef, 0xbb, 0xbf]);
  fs.writeFileSync(filePath, Buffer.concat([utf8Bom, Buffer.from(content, 'utf8')]));
}

function readPartial(name) {
  const filePath = path.join(rendererPartialsDir, name);
  if (!fs.existsSync(filePath)) {
    throw new Error(`Partial not found: ${filePath}`);
  }
  return fs.readFileSync(filePath, 'utf-8');
}

const parts = {
  '<!-- CRAWLER_HERO -->': readPartial('crawler-hero.html'),
  '<!-- CRAWLER_BODY -->': readPartial('crawler-body.html'),
  '<!-- ORGANIZER_HERO -->': readPartial('organizer-hero.html'),
  '<!-- ORGANIZER_BODY -->': readPartial('organizer-body.html'),
  '<!-- SUBSCRIPTION_HERO -->': readPartial('subscription-hero.html'),
  '<!-- SUBSCRIPTION_BODY -->': readPartial('subscription-body.html'),
  '<!-- LIBRARYMETADATA_HERO -->': readPartial('librarymetadata-hero.html'),
  '<!-- LIBRARYMETADATA_BODY -->': readPartial('librarymetadata-body.html')
};

const templatePath = path.join(rendererDir, 'index.template.html');
let html = fs.readFileSync(templatePath, 'utf-8');

for (const [placeholder, content] of Object.entries(parts)) {
  if (!html.includes(placeholder)) {
    console.warn(`WARN: Placeholder ${placeholder} not found in template`);
  }
  html = html.replace(placeholder, content);
}

ensureDirectory(rendererGeneratedDir);
writeUtf8WithBom(generatedIndexHtmlPath, html);
console.log(
  `Assembled generated index.html (${(Buffer.byteLength(html) / 1024).toFixed(1)} KB): ${generatedIndexHtmlPath}`
);
